package api

import (
	"context"
	"fmt"
	"net/http"
	"recipe_importer_ai/services"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	Apify   *services.ApifyService
	Gemini  *services.GeminiService
	Tandoor *services.TandoorService
}

func (h *Handler) ShowIndex(c echo.Context) error {
	html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Recipe Importer AI</title>
    <style>
        body { font-family: sans-serif; max-width: 600px; margin: 40px auto; padding: 20px; line-height: 1.6; }
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 5px; font-weight: bold; }
        select, input[type="text"] { width: 100%; padding: 10px; border: 1px solid #ccc; border-radius: 4px; box-sizing: border-box; }
        button { background: #28a745; color: white; border: none; padding: 12px 20px; border-radius: 4px; cursor: pointer; font-size: 16px; width: 100%; }
        button:hover { background: #218838; }
        #status { margin-top: 20px; padding: 15px; border-radius: 4px; display: none; }
        .success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
    </style>
</head>
<body>
    <h1>Recipe Importer AI</h1>
    <div class="form-group">
        <label for="space">Select Tandoor Space:</label>
        <select id="space">
            <option value="">Loading...</option>
        </select>
    </div>
    <div class="form-group">
        <label for="url">Recipe or Profile URL:</label>
        <input type="text" id="url" placeholder="https://www.instagram.com/p/..." required>
    </div>
    <button id="importBtn">Import Recipe</button>

    <div id="status"></div>

    <script>
        const spaceSelect = document.getElementById('space');
        const urlInput = document.getElementById('url');
        const importBtn = document.getElementById('importBtn');
        const statusDiv = document.getElementById('status');

        // Fetch spaces
        fetch('/api/spaces')
            .then(res => res.json())
            .then(data => {
                spaceSelect.innerHTML = '';
                data.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s.id;
                    opt.textContent = s.name;
                    spaceSelect.appendChild(opt);
                });
            })
            .catch(err => {
                spaceSelect.innerHTML = '<option value="">Error loading spaces</option>';
            });

        importBtn.addEventListener('click', () => {
            const url = urlInput.value;
            const space = spaceSelect.value;
            if (!url) return alert('Please provide a URL!');

            statusDiv.style.display = 'block';
            statusDiv.className = '';
            statusDiv.textContent = 'Starting import...';
            importBtn.disabled = true;

            fetch('/import?url=' + encodeURIComponent(url) + '&space=' + space)
                .then(res => {
                    if (res.status === 202) {
                        statusDiv.className = 'success';
                        statusDiv.textContent = 'Import task submitted! It is running in the background. Check Tandoor in a moment.';
                        urlInput.value = '';
                    } else {
                        throw new Error('Server error');
                    }
                })
                .catch(err => {
                    statusDiv.className = 'error';
                    statusDiv.textContent = 'An error occurred while scheduling the import.';
                })
                .finally(() => {
                    importBtn.disabled = false;
                });
        });
    </script>
</body>
</html>
	`
	return c.HTML(http.StatusOK, html)
}

func (h *Handler) GetSpaces(c echo.Context) error {
	correlationID := c.Request().Header.Get("X-Correlation-ID")
	spaces, err := h.Tandoor.GetSpaces(correlationID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, spaces)
}

func (h *Handler) ImportRecipe(c echo.Context) error {
	url := c.QueryParam("url")
	spaceID := c.QueryParam("space")
	if url == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "url parameter is required"})
	}

	correlationID := c.Request().Header.Get("X-Correlation-ID")
	services.LogJSON(correlationID, "API", fmt.Sprintf("Received import request for URL: %s in space %s", url, spaceID), "INFO")

	go h.ProcessURL(url, spaceID, correlationID)

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"message":        "Import started",
		"correlation_id": correlationID,
	})
}

func (h *Handler) ProcessURL(url string, spaceID string, cid string) {
	services.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for URL: %s", url), "INFO")

	items, err := h.Apify.ScrapeItems(url, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Final failure at Scrape stage for %s: %v", url, err), "ERROR")
		return
	}

	if len(items) > 1 {
		services.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d), processing as profile/batch sequentially", len(items)), "INFO")
		for _, item := range items {
			h.processScrapedItem(item, spaceID, cid)
		}
	} else if len(items) == 1 {
		h.processScrapedItem(items[0], spaceID, cid)
	} else {
		services.LogJSON(cid, "Background", "No items found to process", "WARN")
	}
}

func (h *Handler) processScrapedItem(item services.ScrapedItem, spaceID string, cid string) {
	ctx := context.Background()
	
	recipe, err := h.Gemini.ProcessRecipe(ctx, item.Text, cid)
	if err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage for %s: %v", item.URL, err), "ERROR")
		return
	}
	
	if recipe == nil {
		return
	}

	recipe.SourceURL = item.URL
	recipe.ImageURL = item.ImageURL

	if err := h.Tandoor.SaveRecipe(recipe, spaceID, cid); err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for %s: %v", item.URL, err), "ERROR")
		return
	}

	services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
}
