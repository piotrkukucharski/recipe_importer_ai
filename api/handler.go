package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"recipe_importer_ai/services"
	"time"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*
var templatesFS embed.FS

var templates = template.Must(template.ParseFS(templatesFS, "templates/index.html", "templates/progress.html"))

type Handler struct {
	Apify         *services.ApifyService
	Gemini        *services.GeminiService
	Tandoor       *services.TandoorService
}

func (h *Handler) getToken(c echo.Context) string {
	cookie, err := c.Cookie("tandoor_token")
	if err == nil {
		return cookie.Value
	}
	return ""
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
	return templates.ExecuteTemplate(c.Response().Writer, "index.html", nil)
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
	services.LogJSON(correlationID, "API", fmt.Sprintf("Received import request for URL: %s in space %s (Lang: %s)", url, spaceID, lang), "INFO")

	go h.ProcessURL(url, spaceID, lang, token, correlationID)

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

func (h *Handler) ProcessURL(url string, spaceID string, lang string, token string, cid string) {
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for URL: %s", url), "INFO")

	items, err := h.Apify.ScrapeItems(url, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Final failure at Scrape stage for %s: %v", url, err), "ERROR")
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
	}
}

func (h *Handler) processScrapedItem(item services.ScrapedItem, spaceID string, lang string, token string, cid string) {
	ctx := context.Background()

	fullText := item.Text

	recipe, err := h.Gemini.ProcessRecipe(ctx, fullText, item.Images, lang, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage for %s: %v", item.URL, err), "ERROR")
		return
	}

	if recipe == nil {
		return
	}

	recipe.SourceURL = item.URL

    // Visual image selection
    bestImage := ""
    maxScore := -1
    
    // Limit to top 5 candidates to avoid excessive API calls and time
    candidates := item.Images
    if len(candidates) > 5 {
        candidates = candidates[:5]
    }

    services.LogJSON(cid, "Background", fmt.Sprintf("Starting visual evaluation for %d image candidates", len(candidates)), "INFO")
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
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for %s: %v", item.URL, err), "ERROR")
		return
	}

    if createdRecipe != nil {
        services.BroadcastRecipe(cid, createdRecipe)
	    services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
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
	return templates.ExecuteTemplate(c.Response().Writer, "progress.html", data)
}
