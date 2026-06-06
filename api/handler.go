package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"recipe_importer_ai/services"
	"runtime/debug"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*
var templatesFS embed.FS

var templates = template.Must(template.ParseFS(templatesFS, "templates/index.html", "templates/progress.html", "templates/imports.html"))

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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received import request for URL: %s in space %s (Lang: %s)", url, spaceName, lang), "INFO")

	go h.ProcessURL(url, spaceID, spaceName, username, lang, token, correlationID)

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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received text import request in space %s (Lang: %s)", spaceName, req.Lang), "INFO")

	go h.ProcessText(req.Text, req.SpaceID, spaceName, username, req.Lang, token, correlationID)

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

	services.LogJSON(correlationID, "API", fmt.Sprintf("Received image import request for %d images in space %s (Lang: %s)", len(files), spaceName, lang), "INFO")

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

	go h.ProcessImages(images, mimeTypes, spaceID, spaceName, username, lang, token, correlationID)

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

func (h *Handler) ProcessImages(images [][]byte, mimeTypes []string, spaceID string, spaceName string, username string, lang string, token string, cid string) {
	h.addImport("Import from Images", cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for %d images", len(images)), "INFO")

	ctx := context.Background()

	recipes, err := h.Gemini.ProcessRecipeFromImages(ctx, images, mimeTypes, lang, cid)
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

func (h *Handler) ProcessText(text string, spaceID string, spaceName string, username string, lang string, token string, cid string) {
	h.addImport("Import from Text", cid, username, spaceName)
	services.LogJSON(cid, "Background", "Starting processing for raw text", "INFO")

	ctx := context.Background()

	recipes, err := h.Gemini.ProcessRecipe(ctx, text, []string{}, lang, cid)
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

func (h *Handler) ProcessURL(url string, spaceID string, spaceName string, username string, lang string, token string, cid string) {
	h.addImport(url, cid, username, spaceName)
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for URL: %s", url), "INFO")

	items, err := h.Apify.ScrapeItems(url, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Final failure at Scrape stage for %s: %v", url, err), "ERROR")
		h.updateImportStatus(cid, "finished")
		return
	}

	if len(items) > 1 {
		services.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d), processing as profile/batch sequentially", len(items)), "INFO")
		for _, item := range items {
			h.processScrapedItem(item, spaceID, lang, token, cid)
		}
	} else if len(items) == 1 {
		h.processScrapedItem(items[0], spaceID, lang, token, cid)
	} else {
		services.LogJSON(cid, "Background", "No items found to process", "WARN")
		h.updateImportStatus(cid, "finished")
	}
}

func (h *Handler) processScrapedItem(item services.ScrapedItem, spaceID string, lang string, token string, cid string) {
	ctx := context.Background()

	fullText := item.Text

	recipes, err := h.Gemini.ProcessRecipe(ctx, fullText, item.Images, lang, cid)
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
