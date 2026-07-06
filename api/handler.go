package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"recipe_importer_ai/services"
	"runtime/debug"
	"sync"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*
var templatesFS embed.FS

var templates = template.Must(template.ParseFS(templatesFS, "templates/index.html", "templates/progress.html", "templates/imports.html", "templates/tools.html"))

var (
	VersionBranch    = "unknown"
	VersionTag       = ""
	VersionCommit    = "unknown"
	VersionBuildDate = "unknown"
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if VersionCommit == "unknown" {
					VersionCommit = setting.Value
				}
			case "vcs.time":
				if VersionBuildDate == "unknown" {
					VersionBuildDate = setting.Value
				}
			}
		}
	}
}

func (h *Handler) getTemplateData(extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"VersionBranch":    VersionBranch,
		"VersionTag":       VersionTag,
		"VersionCommit":    VersionCommit,
		"VersionBuildDate": VersionBuildDate,
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

type ImportTask struct {
	URL           string    `json:"url"`
	CorrelationID string    `json:"correlation_id"`
	Status        string    `json:"status"` // "started", "imported", "finished"
	CreatedAt     time.Time `json:"created_at"`
	User          string    `json:"user"`
	Space         string    `json:"space"`
}

type ImportTextRequest struct {
	Text    string `json:"text"`
	SpaceID string `json:"space"`
	Lang    string `json:"lang"`
	Multi   bool   `json:"multi"`
}

type Handler struct {
	Apify           *services.ApifyService
	Gemini          *services.GeminiService
	Tandoor         *services.TandoorService
	imports         []*ImportTask
	importsMu       sync.Mutex
	tokenToUsername map[string]string
	tokenToUserMu   sync.Mutex
}

func (h *Handler) getToken(c echo.Context) string {
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

func (h *Handler) getUsername(token string) string {
	h.tokenToUserMu.Lock()
	defer h.tokenToUserMu.Unlock()
	if name, exists := h.tokenToUsername[token]; exists {
		return name
	}
	return "Active User"
}

func (h *Handler) Login(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token, err := h.Tandoor.Authenticate(req.Username, req.Password)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
	}

	h.tokenToUserMu.Lock()
	if h.tokenToUsername == nil {
		h.tokenToUsername = make(map[string]string)
	}
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

func (h *Handler) Logout(c echo.Context) error {
	token := h.getToken(c)
	if token != "" {
		h.tokenToUserMu.Lock()
		if h.tokenToUsername != nil {
			delete(h.tokenToUsername, token)
		}
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

func (h *Handler) GetLogs(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	logChan := services.Subscribe()
	defer services.Unsubscribe(logChan)

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

func (h *Handler) GetLogsByCorrelationID(c echo.Context) error {
	targetCID := c.Param("CorrelationID")
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set(echo.HeaderCacheControl, "no-cache")
	c.Response().Header().Set(echo.HeaderConnection, "keep-alive")
	c.Response().WriteHeader(http.StatusOK)

	logChan := services.Subscribe()
	defer services.Unsubscribe(logChan)

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

func (h *Handler) ShowIndex(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return templates.ExecuteTemplate(c.Response().Writer, "index.html", h.getTemplateData(nil))
}

func (h *Handler) addImport(url, cid, user, space string) {
	h.importsMu.Lock()
	defer h.importsMu.Unlock()
	
	// Avoid duplicate entries
	for _, imp := range h.imports {
		if imp.CorrelationID == cid {
			return
		}
	}

	h.imports = append(h.imports, &ImportTask{
		URL:           url,
		CorrelationID: cid,
		Status:        "started",
		CreatedAt:     time.Now(),
		User:          user,
		Space:         space,
	})

	// Limit history to 100 elements
	if len(h.imports) > 100 {
		h.imports = h.imports[len(h.imports)-100:]
	}
}

func (h *Handler) resolveSpaceName(spaceID string, token string, correlationID string) string {
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

func (h *Handler) updateImportStatus(cid string, status string) {
	h.importsMu.Lock()
	defer h.importsMu.Unlock()
	for _, imp := range h.imports {
		if imp.CorrelationID == cid {
			imp.Status = status
			break
		}
	}
}

func (h *Handler) ShowImports(c echo.Context) error {
	h.importsMu.Lock()
	defer h.importsMu.Unlock()

	// Reverse imports to show newest first
	n := len(h.imports)
	reversed := make([]*ImportTask, n)
	for i, imp := range h.imports {
		reversed[n-1-i] = imp
	}

	data := map[string]interface{}{
		"Imports": reversed,
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return templates.ExecuteTemplate(c.Response().Writer, "imports.html", h.getTemplateData(data))
}

func (h *Handler) GetSpaces(c echo.Context) error {
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

func (h *Handler) ImportRecipe(c echo.Context) error {
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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received import request for URL: %s in space %s (Lang: %s, Multi-Recipe: %v)", url, spaceName, lang, multi), "INFO")

	go h.ProcessURL(url, spaceID, spaceName, username, lang, multi, token, correlationID)

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

func (h *Handler) ImportRecipeFromText(c echo.Context) error {
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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received text import request in space %s (Lang: %s, Multi-Recipe: %v)", spaceName, req.Lang, req.Multi), "INFO")

	go h.ProcessText(req.Text, req.SpaceID, spaceName, username, req.Lang, req.Multi, token, correlationID)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
		"debug": map[string]interface{}{
			"space_id": req.SpaceID,
			"lang":     req.Lang,
		},
	})
}

func (h *Handler) ImportRecipeFromImages(c echo.Context) error {
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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received image import request for %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")

	// Read all files into memory
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

	go h.ProcessImages(images, mimeTypes, spaceID, spaceName, username, lang, multi, token, correlationID)

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

func (h *Handler) ImportRecipeCustom(c echo.Context) error {
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

	if len(files) > 0 && text != "" {
		// Read all files into memory
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

		services.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with text and %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")
		go h.ProcessTextAndImages(images, mimeTypes, text, spaceID, spaceName, username, lang, multi, token, correlationID)
	} else if len(files) > 0 {
		// Only images
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

		services.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with %d images in space %s (Lang: %s, Multi-Recipe: %v)", len(files), spaceName, lang, multi), "INFO")
		go h.ProcessImages(images, mimeTypes, spaceID, spaceName, username, lang, multi, token, correlationID)
	} else {
		// Only text
		services.LogJSON(correlationID, "API", fmt.Sprintf("Received custom import request with text in space %s (Lang: %s, Multi-Recipe: %v)", spaceName, lang, multi), "INFO")
		go h.ProcessText(text, spaceID, spaceName, username, lang, multi, token, correlationID)
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

func (h *Handler) ProcessTextAndImages(images [][]byte, mimeTypes []string, text string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	h.addImport("Import from Text & Images", cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for %d images and recipe text (Multi-Recipe: %v)", len(images), multi), "INFO")

	ctx := context.Background()

	recipes, err := h.Gemini.ProcessRecipeFromImagesAndText(ctx, images, mimeTypes, text, lang, multi, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		services.LogJSON(cid, "Background", "No recipes found in the uploaded images and text", "WARN")
		h.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		createdRecipe, err := h.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for recipe %s: %v", recipe.Name, err), "ERROR")
			continue
		}

		if createdRecipe != nil {
			recipeID := int(createdRecipe["id"].(float64))
			
			if recipe.DishImageIndex != nil && *recipe.DishImageIndex >= 0 && *recipe.DishImageIndex < len(images) {
				idx := *recipe.DishImageIndex
				services.LogJSON(cid, "Background", fmt.Sprintf("Gemini identified image at index %d as the finished dish for %s. Uploading to Tandoor...", idx, recipe.Name), "INFO")
				
				err = h.Tandoor.UpdateImageFileMultipartWithRetry(recipeID, images[idx], mimeTypes[idx], spaceID, token, cid)
				if err != nil {
					services.LogJSON(cid, "Background", fmt.Sprintf("Warning: failed to upload recipe image file: %v", err), "WARN")
				}
			}

			services.BroadcastRecipe(cid, createdRecipe)
			services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
			importedCount++
		}
	}

	if importedCount > 0 {
		h.updateImportStatus(cid, "imported")
	} else {
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) ProcessImages(images [][]byte, mimeTypes []string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	h.addImport("Import from Images", cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for %d images (Multi-Recipe: %v)", len(images), multi), "INFO")

	ctx := context.Background()

	recipes, err := h.Gemini.ProcessRecipeFromImages(ctx, images, mimeTypes, lang, multi, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		services.LogJSON(cid, "Background", "No recipes found in the uploaded images", "WARN")
		h.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		createdRecipe, err := h.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for recipe %s: %v", recipe.Name, err), "ERROR")
			continue
		}

		if createdRecipe != nil {
			recipeID := int(createdRecipe["id"].(float64))
			
			if recipe.DishImageIndex != nil && *recipe.DishImageIndex >= 0 && *recipe.DishImageIndex < len(images) {
				idx := *recipe.DishImageIndex
				services.LogJSON(cid, "Background", fmt.Sprintf("Gemini identified image at index %d as the finished dish for %s. Uploading to Tandoor...", idx, recipe.Name), "INFO")
				
				err = h.Tandoor.UpdateImageFileMultipartWithRetry(recipeID, images[idx], mimeTypes[idx], spaceID, token, cid)
				if err != nil {
					services.LogJSON(cid, "Background", fmt.Sprintf("Warning: failed to upload recipe image file: %v", err), "WARN")
				}
			}

			services.BroadcastRecipe(cid, createdRecipe)
			services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
			importedCount++
		}
	}

	if importedCount > 0 {
		h.updateImportStatus(cid, "imported")
	} else {
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) ProcessText(text string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	h.addImport("Import from Text", cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for raw text (Multi-Recipe: %v)", multi), "INFO")

	ctx := context.Background()

	recipes, err := h.Gemini.ProcessRecipe(ctx, text, []string{}, lang, multi, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		services.LogJSON(cid, "Background", "No recipes found in the provided text", "WARN")
		h.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		createdRecipe, err := h.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for recipe %s: %v", recipe.Name, err), "ERROR")
			continue
		}

		if createdRecipe != nil {
			services.BroadcastRecipe(cid, createdRecipe)
			services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
			importedCount++
		}
	}

	if importedCount > 0 {
		h.updateImportStatus(cid, "imported")
	} else {
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) ProcessURL(url string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	h.addImport(url, cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for URL: %s (Multi-Recipe: %v)", url, multi), "INFO")

	items, err := h.Apify.ScrapeItems(url, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Final failure at Scrape stage for %s: %v", url, err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(items) > 1 {
		if multi {
			services.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d), processing as profile/batch sequentially", len(items)), "INFO")
			for _, item := range items {
				h.processScrapedItem(item, spaceID, lang, multi, token, cid)
			}
		} else {
			services.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d) but multi-recipe mode is disabled. Processing only the first item.", len(items)), "INFO")
			h.processScrapedItem(items[0], spaceID, lang, multi, token, cid)
		}
	} else if len(items) == 1 {
		h.processScrapedItem(items[0], spaceID, lang, multi, token, cid)
	} else {
		services.LogJSON(cid, "Background", "No items found to process", "WARN")
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) processScrapedItem(item services.ScrapedItem, spaceID string, lang string, multi bool, token string, cid string) {
	ctx := context.Background()

	fullText := item.Text

	recipes, err := h.Gemini.ProcessRecipe(ctx, fullText, item.Images, lang, multi, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage for %s: %v", item.URL, err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		h.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		recipe.SourceURL = item.URL

		// Visual image selection
		bestImage := ""
		maxScore := -1
		
		// Limit to top 5 candidates to avoid excessive API calls and time
		candidates := item.Images
		if len(candidates) > 5 {
			candidates = candidates[:5]
		}

		services.LogJSON(cid, "Background", fmt.Sprintf("Starting visual evaluation for %d image candidates for %s", len(candidates), recipe.Name), "INFO")
		for _, imgURL := range candidates {
			score, err := h.Gemini.EvaluateImage(ctx, imgURL, recipe.Name, cid)
			if err != nil {
				services.LogJSON(cid, "Background", fmt.Sprintf("Failed to evaluate image %s: %v", imgURL, err), "WARN")
				continue
			}
			services.LogJSON(cid, "Background", fmt.Sprintf("Image score %d for: %s", score, imgURL), "INFO")
			if score > maxScore {
				maxScore = score
				bestImage = imgURL
			}
			if score == 10 { break } // Perfect match found
		}

		if bestImage != "" && maxScore >= 4 { // Threshold of 4 to avoid poor images
			recipe.ImageURL = bestImage
			services.LogJSON(cid, "Background", fmt.Sprintf("Selected best image with score %d: %s", maxScore, bestImage), "INFO")
		} else if recipe.ImageURL == "" && item.ImageURL != "" {
			recipe.ImageURL = item.ImageURL
		}

		createdRecipe, err := h.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for %s (%s): %v", item.URL, recipe.Name, err), "ERROR")
			continue
		}

		if createdRecipe != nil {
			services.BroadcastRecipe(cid, createdRecipe)
			services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
			importedCount++
		}
	}

	if importedCount > 0 {
		h.updateImportStatus(cid, "imported")
	} else {
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) DeleteRecipe(c echo.Context) error {
	recipeID := c.Param("id")
	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	if err := h.Tandoor.DeleteRecipe(recipeID, token, correlationID); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "Recipe deleted"})
}

func (h *Handler) ShowImportProgress(c echo.Context) error {
	cid := c.Param("CorrelationID")
	tandoorURL := os.Getenv("TANDOOR_URL")
	data := map[string]interface{}{
		"CorrelationID": cid,
		"TandoorURL":    tandoorURL,
	}
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return templates.ExecuteTemplate(c.Response().Writer, "progress.html", h.getTemplateData(data))
}

func (h *Handler) ShowTools(c echo.Context) error {
	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	c.Response().WriteHeader(http.StatusOK)
	return templates.ExecuteTemplate(c.Response().Writer, "tools.html", h.getTemplateData(nil))
}

func (h *Handler) GetRecipeBooks(c echo.Context) error {
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

type DuplicateGroup struct {
	Strategy string                   `json:"strategy"`
	Key      string                   `json:"key"`
	Recipes  []map[string]interface{} `json:"recipes"`
}

func (h *Handler) GetDuplicates(c echo.Context) error {
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

	// 1. Find duplicates by Title (case-insensitive)
	titleGroups := make(map[string][]map[string]interface{})
	// 2. Find duplicates by source_url (excluding null / empty)
	urlGroups := make(map[string][]map[string]interface{})

	for _, recipe := range recipes {
		name, _ := recipe["name"].(string)
		nameKey := strings.ToLower(strings.TrimSpace(name))
		if nameKey != "" {
			titleGroups[nameKey] = append(titleGroups[nameKey], recipe)
		}

		if sourceURLVal, exists := recipe["source_url"]; exists && sourceURLVal != nil {
			if sourceURL, ok := sourceURLVal.(string); ok && sourceURL != "" {
				urlGroups[sourceURL] = append(urlGroups[sourceURL], recipe)
			}
		}
	}

	var duplicateGroups []DuplicateGroup

	// Add title duplicate groups (groups with size > 1)
	for key, group := range titleGroups {
		if len(group) > 1 {
			duplicateGroups = append(duplicateGroups, DuplicateGroup{
				Strategy: "title",
				Key:      key,
				Recipes:  group,
			})
		}
	}

	// Add url duplicate groups (groups with size > 1)
	for key, group := range urlGroups {
		if len(group) > 1 {
			duplicateGroups = append(duplicateGroups, DuplicateGroup{
				Strategy: "source_url",
				Key:      key,
				Recipes:  group,
			})
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{"groups": duplicateGroups})
}

type CleanDuplicatesRequest struct {
	SpaceID string           `json:"space_id"`
	Groups  []DuplicateGroup `json:"groups"`
}

func (h *Handler) CleanDuplicates(c echo.Context) error {
	var req CleanDuplicatesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	deletedCount := 0
	for _, group := range req.Groups {
		if len(group.Recipes) <= 1 {
			continue
		}

		// Sort recipes by ID descending (newest ID first)
		recipes := group.Recipes
		for i := 0; i < len(recipes); i++ {
			for j := i + 1; j < len(recipes); j++ {
				idI := getRecipeID(recipes[i])
				idJ := getRecipeID(recipes[j])
				if idJ > idI {
					recipes[i], recipes[j] = recipes[j], recipes[i]
				}
			}
		}

		// Keep the first one (newest), delete all the rest
		for i := 1; i < len(recipes); i++ {
			recipeID := getRecipeID(recipes[i])
			if recipeID > 0 {
				recipeIDStr := fmt.Sprintf("%d", recipeID)
				services.LogJSON(correlationID, "Tools", fmt.Sprintf("Cleaning duplicate recipe ID %s (%s) from group key '%s'", recipeIDStr, recipes[i]["name"], group.Key), "INFO")
				err := h.Tandoor.DeleteRecipe(recipeIDStr, token, correlationID)
				if err != nil {
					services.LogJSON(correlationID, "Tools", fmt.Sprintf("Failed to delete recipe duplicate %s: %v", recipeIDStr, err), "ERROR")
				} else {
					deletedCount++
				}
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":       "Cleanup completed",
		"deleted_count": deletedCount,
	})
}

func getRecipeID(recipe map[string]interface{}) int {
	if idVal, exists := recipe["id"]; exists {
		if idFloat, ok := idVal.(float64); ok {
			return int(idFloat)
		} else if idInt, ok := idVal.(int); ok {
			return idInt
		}
	}
	return 0
}

type SuggestBookRecipesRequest struct {
	SpaceID string `json:"space_id"`
	BookID  int    `json:"book_id"`
}

func (h *Handler) SuggestBookRecipes(c echo.Context) error {
	var req SuggestBookRecipesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	// 1. Get book details
	book, err := h.Tandoor.GetRecipeBook(req.BookID, req.SpaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to get book: %v", err)})
	}
	bookName, _ := book["name"].(string)
	bookDesc, _ := book["description"].(string)

	// 2. Get book entries
	bookEntries, err := h.Tandoor.GetRecipeBookEntries(req.BookID, req.SpaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to get book entries: %v", err)})
	}

	inBookMap := make(map[int]bool)
	for _, entry := range bookEntries {
		if recipeIDVal, exists := entry["recipe"]; exists {
			var recipeID int
			if rf, ok := recipeIDVal.(float64); ok {
				recipeID = int(rf)
			} else if ri, ok := recipeIDVal.(int); ok {
				recipeID = ri
			}
			if recipeID > 0 {
				inBookMap[recipeID] = true
			}
		}
	}

	// 3. Get all recipes in space
	allRecipes, err := h.Tandoor.GetRecipes(req.SpaceID, token, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to get recipes: %v", err)})
	}

	var candidates []map[string]interface{}
	for _, r := range allRecipes {
		id := getRecipeID(r)
		if id > 0 && !inBookMap[id] {
			candidates = append(candidates, r)
		}
	}

	if len(candidates) == 0 {
		return c.JSON(http.StatusOK, map[string]interface{}{"suggested_recipes": []interface{}{}})
	}

	// 4. Get 10 newest examples currently in the book
	var existingExamples []map[string]interface{}
	// Sort bookEntries by id descending to get the newest
	for i := 0; i < len(bookEntries); i++ {
		for j := i + 1; j < len(bookEntries); j++ {
			idI := getRecipeID(bookEntries[i])
			idJ := getRecipeID(bookEntries[j])
			if idJ > idI {
				bookEntries[i], bookEntries[j] = bookEntries[j], bookEntries[i]
			}
		}
	}
	for i := 0; i < len(bookEntries) && len(existingExamples) < 10; i++ {
		entry := bookEntries[i]
		if recipeContent, ok := entry["recipe_content"].(map[string]interface{}); ok {
			existingExamples = append(existingExamples, recipeContent)
		}
	}

	// 5. Ask Gemini to classify
	// Chunk candidate list if it exceeds 100 to keep it fast
	if len(candidates) > 100 {
		candidates = candidates[:100]
	}

	ctx := context.Background()
	matchedIDs, err := h.Gemini.ClassifyRecipesForBook(ctx, bookName, bookDesc, existingExamples, candidates, correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Gemini classification failed: %v", err)})
	}

	matchedMap := make(map[int]bool)
	for _, id := range matchedIDs {
		matchedMap[id] = true
	}

	var suggestedRecipes []map[string]interface{}
	for _, r := range candidates {
		id := getRecipeID(r)
		if id > 0 && matchedMap[id] {
			suggestedRecipes = append(suggestedRecipes, r)
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"suggested_recipes": suggestedRecipes,
	})
}

type AddRecipesToBookRequest struct {
	SpaceID   string `json:"space_id"`
	BookID    int    `json:"book_id"`
	RecipeIDs []int  `json:"recipe_ids"`
}

func (h *Handler) AddRecipesToBook(c echo.Context) error {
	var req AddRecipesToBookRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request"})
	}

	token := h.getToken(c)
	if token == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
	}
	correlationID := c.Request().Header.Get("X-Correlation-ID")

	addedCount := 0
	for _, recipeID := range req.RecipeIDs {
		services.LogJSON(correlationID, "Tools", fmt.Sprintf("Adding recipe ID %d to book %d", recipeID, req.BookID), "INFO")
		_, err := h.Tandoor.AddRecipeToBook(req.BookID, recipeID, req.SpaceID, token, correlationID)
		if err != nil {
			services.LogJSON(correlationID, "Tools", fmt.Sprintf("Failed to add recipe %d to book %d: %v", recipeID, req.BookID, err), "ERROR")
		} else {
			addedCount++
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":     "Recipes added successfully",
		"added_count": addedCount,
	})
}
