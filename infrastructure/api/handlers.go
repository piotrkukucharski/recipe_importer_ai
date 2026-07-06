package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/infrastructure/web"
	"recipe_importer_ai/usecases/auth"
	"recipe_importer_ai/usecases/cleanup"
	"recipe_importer_ai/usecases/cookbook"
	"recipe_importer_ai/usecases/copy_space"
	"recipe_importer_ai/usecases/duplicates"
	"recipe_importer_ai/usecases/import_recipe"
	"recipe_importer_ai/usecases/recipe"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type ApiHandler struct {
	Tandoor           *tandoor.TandoorService
	AuthUC            *auth.AuthUseCase
	FindDuplicatesUC  *duplicates.FindUseCase
	CleanDuplicatesUC *duplicates.CleanUseCase
	SuggestUC         *cookbook.SuggestUseCase
	AddRecipesUC      *cookbook.AddUseCase
	ImportURLUC       *import_recipe.ImportURLUseCase
	ImportTextUC      *import_recipe.ImportTextUseCase
	ImportImageUC     *import_recipe.ImportImageUseCase
	TaskManager       *import_recipe.TaskManager
	DeleteRecipeUC    *recipe.DeleteUseCase
	CopyUC            *copy_space.CopyUseCase
	CleanupUC         *cleanup.CleanupUseCase

	tokenToUsername map[string]string
	tokenToUserMu   sync.Mutex
}

func NewApiHandler(
	t *tandoor.TandoorService,
	a *auth.AuthUseCase,
	fd *duplicates.FindUseCase,
	cd *duplicates.CleanUseCase,
	s *cookbook.SuggestUseCase,
	ar *cookbook.AddUseCase,
	iu *import_recipe.ImportURLUseCase,
	it *import_recipe.ImportTextUseCase,
	ii *import_recipe.ImportImageUseCase,
	tm *import_recipe.TaskManager,
	dr *recipe.DeleteUseCase,
	cp *copy_space.CopyUseCase,
	cl *cleanup.CleanupUseCase,
) *ApiHandler {
	return &ApiHandler{
		Tandoor:           t,
		AuthUC:            a,
		FindDuplicatesUC:  fd,
		CleanDuplicatesUC: cd,
		SuggestUC:         s,
		AddRecipesUC:      ar,
		ImportURLUC:       iu,
		ImportTextUC:      it,
		ImportImageUC:     ii,
		TaskManager:       tm,
		DeleteRecipeUC:    dr,
		CopyUC:            cp,
		CleanupUC:         cl,
		tokenToUsername:   make(map[string]string),
	}
}

func (h *ApiHandler) GetTaskManager() *import_recipe.TaskManager {
	return h.TaskManager
}

func (h *ApiHandler) getToken(c echo.Context) string {
	authHeader := c.Request().Header.Get("Authorization")
	if authHeader != "" {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer ")
		}
		return authHeader
	}
	cookie, err := c.Cookie("tandoor_token")
	if err == nil {
		return cookie.Value
	}
	return ""
}

func (h *ApiHandler) getUsername(token string) string {
	h.tokenToUserMu.Lock()
	defer h.tokenToUserMu.Unlock()
	if name, exists := h.tokenToUsername[token]; exists {
		return name
	}
	return "Active User"
}

func (h *ApiHandler) resolveSpaceName(spaceID string, token string, correlationID string) string {
	spaces, err := h.Tandoor.GetSpaces(token, correlationID)
	if err == nil {
		for _, sp := range spaces {
			if fmt.Sprintf("%d", sp.ID) == spaceID {
				return sp.Name
			}
		}
	}
	return spaceID
}

func (h *ApiHandler) ShowIndex(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return web.Templates.ExecuteTemplate(c.Response().Writer, "index.html", web.GetTemplateData(nil))
}

func (h *ApiHandler) ShowImports(c echo.Context) error {
	imports := h.TaskManager.GetTasks()
	n := len(imports)
	reversed := make([]*import_recipe.ImportTask, n)
	for i, imp := range imports {
		reversed[n-1-i] = imp
	}

	data := map[string]interface{}{
		"Imports": reversed,
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return web.Templates.ExecuteTemplate(c.Response().Writer, "imports.html", web.GetTemplateData(data))
}

func (h *ApiHandler) ShowTools(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return web.Templates.ExecuteTemplate(c.Response().Writer, "tools.html", web.GetTemplateData(nil))
}

func (h *ApiHandler) GetSpaces(c echo.Context) error {
	correlationID := c.Request().Header.Get("X-Correlation-ID")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	spaces, err := h.Tandoor.GetSpaces(token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, spaces)
}

func (h *ApiHandler) Login(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	ctx := c.Request().Context()
	token, err := h.AuthUC.Authenticate(ctx, req.Username, req.Password)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
	}

	h.tokenToUserMu.Lock()
	h.tokenToUsername[token] = req.Username
	h.tokenToUserMu.Unlock()

	cookie := new(http.Cookie)
	cookie.Name = "tandoor_token"
	cookie.Value = token
	cookie.Expires = time.Now().Add(24 * 7 * time.Hour) // 1 week
	cookie.Path = "/"
	cookie.HttpOnly = true
	c.SetCookie(cookie)

	return c.JSON(http.StatusOK, map[string]string{"message": "Login successful"})
}

func (h *ApiHandler) Logout(c echo.Context) error {
	token := h.getToken(c)
	if token != "" {
		h.tokenToUserMu.Lock()
		delete(h.tokenToUsername, token)
		h.tokenToUserMu.Unlock()
	}

	cookie := new(http.Cookie)
	cookie.Name = "tandoor_token"
	cookie.Value = ""
	cookie.Expires = time.Now().Add(-1 * time.Hour)
	cookie.Path = "/"
	cookie.HttpOnly = true
	c.SetCookie(cookie)

	return c.JSON(http.StatusOK, map[string]string{"message": "Logged out"})
}

func (h *ApiHandler) GetLogs(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	logChan := logger.Subscribe()
	defer logger.Unsubscribe(logChan)

	enc := json.NewEncoder(c.Response())

	for {
		select {
		case entry := <-logChan:
			fmt.Fprintf(c.Response(), "data: ")
			if err := enc.Encode(entry); err != nil {
				return err
			}
			fmt.Fprintf(c.Response(), "\n\n")
			c.Response().Flush()
		case <-c.Request().Context().Done():
			return nil
		}
	}
}

func (h *ApiHandler) GetLogsByCorrelationID(c echo.Context) error {
	targetCID := c.Param("CorrelationID")
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	logChan := logger.Subscribe()
	defer logger.Unsubscribe(logChan)

	enc := json.NewEncoder(c.Response())

	for {
		select {
		case entry := <-logChan:
			if entry.CorrelationID == targetCID {
				fmt.Fprintf(c.Response(), "data: ")
				if err := enc.Encode(entry); err != nil {
					return err
				}
				fmt.Fprintf(c.Response(), "\n\n")
				c.Response().Flush()
			}
		case <-c.Request().Context().Done():
			return nil
		}
	}
}

func (h *ApiHandler) ImportRecipe(c echo.Context) error {
	url := c.QueryParam("url")
	spaceID := c.QueryParam("space")
	lang := c.QueryParam("lang")
	multi := c.QueryParam("multi") == "true"
	if url == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "url parameter is required"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	username := h.getUsername(token)
	spaceName := h.resolveSpaceName(spaceID, token, correlationID)

	logger.LogJSON(correlationID, "API", fmt.Sprintf("Received import request for URL: %s in space %s (Lang: %s, Multi-Recipe: %v)", url, spaceName, lang, multi), "INFO")

	ctx := context.Background()
	go h.ImportURLUC.Execute(ctx, url, spaceID, spaceName, username, lang, multi, token, correlationID)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
		"debug": map[string]interface{}{
			"url":      url,
			"space_id": spaceID,
			"lang":     lang,
		},
	})
}

func (h *ApiHandler) ImportRecipeFromText(c echo.Context) error {
	var req ImportTextRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request payload"})
	}

	if req.Text == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "text is required"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	username := h.getUsername(token)
	spaceName := h.resolveSpaceName(req.SpaceID, token, correlationID)

	logger.LogJSON(correlationID, "API", fmt.Sprintf("Received text import request in space %s (Lang: %s, Multi-Recipe: %v)", spaceName, req.Lang, req.Multi), "INFO")

	ctx := context.Background()
	go h.ImportTextUC.Execute(ctx, req.Text, req.SpaceID, spaceName, username, req.Lang, req.Multi, token, correlationID)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
		"debug": map[string]interface{}{
			"space_id": req.SpaceID,
			"lang":     req.Lang,
		},
	})
}

func (h *ApiHandler) ImportRecipeFromImages(c echo.Context) error {
	form, err := c.MultipartForm()
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to parse multipart form"})
	}

	spaceID := c.FormValue("space")
	lang := c.FormValue("lang")
	multi := c.FormValue("multi") == "true"

	files := form.File["images"]
	if len(files) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "No images provided"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = fmt.Sprintf("img-%d", time.Now().UnixNano())
	}
	
	username := h.getUsername(token)
	spaceName := h.resolveSpaceName(spaceID, token, correlationID)

	logger.LogJSON(correlationID, "API", fmt.Sprintf("Received image import request for %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")

	var images [][]byte
	var mimeTypes []string
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to open file: %v", err)})
		}
		
		imgData, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to read file: %v", err)})
		}
		
		images = append(images, imgData)
		
		mimeType := fileHeader.Header.Get("Content-Type")
		if mimeType == "" {
			mimeType = http.DetectContentType(imgData)
		}
		mimeTypes = append(mimeTypes, mimeType)
	}

	ctx := context.Background()
	go h.ImportImageUC.ExecuteImages(ctx, images, mimeTypes, spaceID, spaceName, username, lang, multi, token, correlationID)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
		"debug": map[string]interface{}{
			"space_id":    spaceID,
			"lang":        lang,
			"image_count": len(files),
		},
	})
}

func (h *ApiHandler) ImportRecipeCustom(c echo.Context) error {
	form, err := c.MultipartForm()
	
	var spaceID, lang, text string
	var multi bool
	var files []*multipart.FileHeader

	if err == nil && form != nil {
		spaceID = c.FormValue("space")
		lang = c.FormValue("lang")
		text = c.FormValue("text")
		multi = c.FormValue("multi") == "true"
		files = form.File["images"]
	} else {
		spaceID = c.FormValue("space")
		lang = c.FormValue("lang")
		text = c.FormValue("text")
		multi = c.FormValue("multi") == "true"
	}

	if text == "" && len(files) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Either recipe text or images must be provided"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = fmt.Sprintf("cust-%d", time.Now().UnixNano())
	}
	
	username := h.getUsername(token)
	spaceName := h.resolveSpaceName(spaceID, token, correlationID)

	ctx := context.Background()

	if len(files) > 0 && text != "" {
		var images [][]byte
		var mimeTypes []string
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to open file: %v", err)})
			}
			
			imgData, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to read file: %v", err)})
			}
			
			images = append(images, imgData)
			
			mimeType := fileHeader.Header.Get("Content-Type")
			if mimeType == "" {
				mimeType = http.DetectContentType(imgData)
			}
			mimeTypes = append(mimeTypes, mimeType)
		}

		logger.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with text and %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")
		go h.ImportImageUC.ExecuteTextAndImages(ctx, images, mimeTypes, text, spaceID, spaceName, username, lang, multi, token, correlationID)
	} else if len(files) > 0 {
		var images [][]byte
		var mimeTypes []string
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to open file: %v", err)})
			}
			
			imgData, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to read file: %v", err)})
			}
			
			images = append(images, imgData)
			
			mimeType := fileHeader.Header.Get("Content-Type")
			if mimeType == "" {
				mimeType = http.DetectContentType(imgData)
			}
			mimeTypes = append(mimeTypes, mimeType)
		}

		logger.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")
		go h.ImportImageUC.ExecuteImages(ctx, images, mimeTypes, spaceID, spaceName, username, lang, multi, token, correlationID)
	} else {
		logger.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with text in space %s (Lang: %s, Multi-Recipe: %v)", spaceName, lang, multi), "INFO")
		go h.ImportTextUC.Execute(ctx, text, spaceID, spaceName, username, lang, multi, token, correlationID)
	}

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
		"debug": map[string]interface{}{
			"space_id":    spaceID,
			"lang":        lang,
			"has_text":    text != "",
			"image_count": len(files),
			"multi":       multi,
		},
	})
}

func (h *ApiHandler) ShowImportProgress(c echo.Context) error {
	cid := c.Param("CorrelationID")
	tandoorURL := os.Getenv("TANDOOR_URL")
	data := map[string]interface{}{
		"CorrelationID": cid,
		"TandoorURL":    tandoorURL,
	}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return web.Templates.ExecuteTemplate(c.Response().Writer, "progress.html", web.GetTemplateData(data))
}

func (h *ApiHandler) DeleteRecipe(c echo.Context) error {
	recipeID := c.Param("id")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	ctx := c.Request().Context()
	if err := h.DeleteRecipeUC.Execute(ctx, "", recipeID, token, correlationID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe deleted"})
}

func (h *ApiHandler) GetRecipeBooks(c echo.Context) error {
	spaceID := c.QueryParam("space_id")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	books, err := h.Tandoor.GetRecipeBooks(spaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, books)
}

func (h *ApiHandler) GetDuplicates(c echo.Context) error {
	spaceID := c.QueryParam("space_id")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	ctx := c.Request().Context()
	groups, err := h.FindDuplicatesUC.Execute(ctx, spaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"groups": groups})
}

func (h *ApiHandler) CleanDuplicates(c echo.Context) error {
	var req struct {
		SpaceID string                       `json:"space_id"`
		Groups  []duplicates.DuplicateGroup `json:"groups"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	ctx := c.Request().Context()
	deletedCount, err := h.CleanDuplicatesUC.Execute(ctx, req.SpaceID, req.Groups, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":       "Cleanup completed",
		"deleted_count": deletedCount,
	})
}

func (h *ApiHandler) SuggestBookRecipesStream(c echo.Context) error {
	spaceID := c.QueryParam("space_id")
	bookIDStr := c.QueryParam("book_id")
	var bookID int
	fmt.Sscanf(bookIDStr, "%d", &bookID)

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set(echo.HeaderCacheControl, "no-cache")
	w.Header().Set(echo.HeaderConnection, "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendEvent := func(event string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w.Writer, "event: %s\ndata: %s\n\n", event, string(payload))
		w.Flush()
	}

	sendStatus := func(status string) {
		sendEvent("status", map[string]string{"message": status})
	}

	ctx := c.Request().Context()
	recipes, err := h.SuggestUC.Suggest(ctx, spaceID, bookID, token, correlationID, sendStatus)
	if err != nil {
		sendEvent("error", map[string]string{"message": fmt.Sprintf("Failed: %v", err)})
		return nil
	}

	sendEvent("recipes", recipes)
	sendStatus("Complete")
	return nil
}

func (h *ApiHandler) AddRecipesToBook(c echo.Context) error {
	var req struct {
		SpaceID   string `json:"space_id"`
		BookID    int    `json:"book_id"`
		RecipeIDs []int  `json:"recipe_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	ctx := c.Request().Context()
	addedCount, err := h.AddRecipesUC.Execute(ctx, req.BookID, req.RecipeIDs, req.SpaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":     "Recipes added successfully",
		"added_count": addedCount,
	})
}

type ImportTextRequest struct {
	Text    string `json:"text"`
	SpaceID string `json:"space"`
	Lang    string `json:"lang"`
	Multi   bool   `json:"multi"`
}

func (h *ApiHandler) GetSpaceRecipes(c echo.Context) error {
	spaceID := c.QueryParam("space_id")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	recipes, err := h.Tandoor.GetRecipes(spaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, recipes)
}

func (h *ApiHandler) CopySpaceStream(c echo.Context) error {
	mode := c.QueryParam("mode") // "books" or "recipes"
	sourceSpace := c.QueryParam("source_space")
	targetSpace := c.QueryParam("target_space")
	targetLang := c.QueryParam("target_lang")
	importTags := c.QueryParam("import_tags") != "false"

	idsStr := c.QueryParam("ids")
	var ids []int
	for _, idStr := range strings.Split(idsStr, ",") {
		var id int
		if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set(echo.HeaderCacheControl, "no-cache")
	w.Header().Set(echo.HeaderConnection, "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendEvent := func(event string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w.Writer, "event: %s\ndata: %s\n\n", event, string(payload))
		w.Flush()
	}

	sendStatus := func(status string) {
		sendEvent("status", map[string]string{"message": status})
	}

	if len(ids) == 0 {
		sendEvent("error", map[string]string{"message": "No items selected"})
		return nil
	}

	ctx := c.Request().Context()
	err := h.CopyUC.Copy(ctx, mode, ids, sourceSpace, targetSpace, targetLang, importTags, token, correlationID, sendStatus)
	if err != nil {
		sendEvent("error", map[string]string{"message": err.Error()})
		return nil
	}

	sendStatus("Copy process completed successfully!")
	sendEvent("complete", map[string]string{"message": "Finished"})
	return nil
}

func (h *ApiHandler) CleanupStream(c echo.Context) error {
	itemType := c.QueryParam("type")
	targetLang := c.QueryParam("target_lang")
	spaceID := c.QueryParam("space_id")

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set(echo.HeaderCacheControl, "no-cache")
	w.Header().Set(echo.HeaderConnection, "keep-alive")
	w.WriteHeader(http.StatusOK)

	sendEvent := func(event string, data interface{}) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w.Writer, "event: %s\ndata: %s\n\n", event, string(payload))
		w.Flush()
	}

	sendStatus := func(status string) {
		sendEvent("status", map[string]string{"message": status})
	}

	ctx := c.Request().Context()
	err := h.CleanupUC.Cleanup(ctx, itemType, targetLang, spaceID, token, correlationID, sendStatus)
	if err != nil {
		sendEvent("error", map[string]string{"message": err.Error()})
		return nil
	}

	sendEvent("complete", map[string]string{"message": "Finished"})
	return nil
}
