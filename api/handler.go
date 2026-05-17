package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"recipe_importer_ai/services"
	"time"

	"github.com/labstack/echo/v4"
)

type Handler struct {
	Apify         *services.ApifyService
	Gemini        *services.GeminiService
	Tandoor       *services.TandoorService
	Transcription *services.TranscriptionService
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
	html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Recipe Importer AI</title>
    <style>
        body { font-family: sans-serif; max-width: 600px; margin: 0 auto; padding: 20px; line-height: 1.6; background: #f4f7f6; }
        .container { background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { margin-top: 0; color: #333; }
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 5px; font-weight: bold; color: #555; }
        select, input[type="text"], input[type="password"] { width: 100%; padding: 12px; border: 1px solid #ddd; border-radius: 4px; box-sizing: border-box; font-size: 14px; }
        button { background: #28a745; color: white; border: none; padding: 12px 20px; border-radius: 4px; cursor: pointer; font-size: 16px; width: 100%; transition: background 0.3s; }
        button:hover { background: #218838; }
        button.secondary { background: #6c757d; margin-top: 10px; }
        button.secondary:hover { background: #5a6268; }
        #status { margin-top: 20px; padding: 15px; border-radius: 4px; display: none; }
        .success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        #login-form { display: block; }
        #main-app { display: none; }
        nav { display: none; justify-content: flex-end; margin-bottom: 20px; }
        .logout-btn { background: #dc3545; width: auto; padding: 8px 15px; font-size: 14px; }
        .logout-btn:hover { background: #c82333; }
        #log-container { margin-top: 20px; background: #222; color: #eee; padding: 15px; border-radius: 4px; font-family: monospace; font-size: 12px; height: 300px; overflow-y: auto; display: none; }
        .log-entry { margin-bottom: 4px; border-bottom: 1px solid #333; padding-bottom: 2px; }
        .log-time { color: #888; margin-right: 8px; }
        .log-svc { color: #4fc3f7; font-weight: bold; margin-right: 8px; width: 80px; display: inline-block; }
        .log-msg { white-space: pre-wrap; }
        .level-ERROR { color: #ff5252; }
        .level-WARN { color: #ffb74d; }
        .level-INFO { color: #81c784; }
    </style>
</head>
<body>
    <nav id="navbar">
        <button id="logoutBtnTop" class="logout-btn">Logout</button>
    </nav>

    <div class="container">
        <h1>Recipe Importer AI</h1>

        <div id="login-form">
            <h3>Login with Tandoor Credentials</h3>
            <div class="form-group">
                <label for="username">Email/Username:</label>
                <input type="text" id="username" required>
            </div>
            <div class="form-group">
                <label for="password">Password:</label>
                <input type="password" id="password" required>
            </div>
            <button id="loginBtn">Login</button>
        </div>

        <div id="main-app">
            <div class="form-group">
                <label for="space">Select Tandoor Space:</label>
                <select id="space">
                    <option value="">Loading...</option>
                </select>
            </div>
            <div class="form-group">
                <label for="lang">Target Language:</label>
                <select id="lang">
                    <option value="Polish">Polish</option>
                    <option value="English">English</option>
                    <option value="German">German</option>
                    <option value="French">French</option>
                    <option value="Spanish">Spanish</option>
                    <option value="Italian">Italian</option>
                </select>
            </div>
            <div class="form-group">
                <label for="url">Recipe or Profile URL:</label>
                <input type="text" id="url" placeholder="https://www.instagram.com/p/..." required>
            </div>
            <button id="importBtn">Import Recipe</button>
        </div>

        <div id="status"></div>
        <div id="log-container"></div>
    </div>

    <script>
        const loginForm = document.getElementById('login-form');
        const mainApp = document.getElementById('main-app');
        const navbar = document.getElementById('navbar');
        const usernameInput = document.getElementById('username');
        const passwordInput = document.getElementById('password');
        const loginBtn = document.getElementById('loginBtn');
        const logoutBtn = document.getElementById('logoutBtnTop');

        const spaceSelect = document.getElementById('space');
        const langSelect = document.getElementById('lang');
        const urlInput = document.getElementById('url');
        const importBtn = document.getElementById('importBtn');
        const statusDiv = document.getElementById('status');
        const logContainer = document.getElementById('log-container');

        let logSource = null;

        function showApp() {
            loginForm.style.display = 'none';
            mainApp.style.display = 'block';
            navbar.style.display = 'flex';
            loadSpaces();
        }

        function showLogin() {
            loginForm.style.display = 'block';
            mainApp.style.display = 'none';
            navbar.style.display = 'none';
            statusDiv.style.display = 'none';
            logContainer.style.display = 'none';
        }

        function addLog(entry) {
            const div = document.createElement('div');
            div.className = 'log-entry';
            
            const time = new Date(entry.timestamp).toLocaleTimeString();
            div.innerHTML = '<span class="log-time">' + time + '</span>' +
                            '<span class="log-svc">[' + entry.service + ']</span>' +
                            '<span class="log-msg level-' + entry.level + '">' + entry.message + '</span>';
            logContainer.appendChild(div);
            logContainer.scrollTop = logContainer.scrollHeight;
        }

        function startLogStream(cid) {
            if (logSource) logSource.close();
            logContainer.innerHTML = '';
            logContainer.style.display = 'block';
            
            logSource = new EventSource('/api/logs/' + cid);
            logSource.onmessage = (e) => {
                const entry = JSON.parse(e.data);
                addLog(entry);
                if (entry.message.includes('Pipeline completed successfully') || entry.message.includes('Final failure')) {
                    // We don't necessarily want to close immediately to let user see final logs
                    // but we could if we wanted to.
                }
            };
            logSource.onerror = () => {
                logSource.close();
            };
        }

        function loadSpaces() {
            fetch('/api/spaces')
                .then(res => {
                    if (res.status === 401) {
                        showLogin();
                        return [];
                    }
                    return res.json();
                })
                .then(data => {
                    if (data && data.length > 0) {
                        spaceSelect.innerHTML = '';
                        data.forEach(s => {
                            const opt = document.createElement('option');
                            opt.value = s.id;
                            opt.textContent = s.name;
                            spaceSelect.appendChild(opt);
                        });
                    }
                })
                .catch(err => {
                    console.error('Failed to load spaces:', err);
                });
        }

        loginBtn.addEventListener('click', () => {
            const username = usernameInput.value;
            const password = passwordInput.value;
            
            fetch('/api/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password })
            })
            .then(res => {
                if (res.ok) {
                    showApp();
                } else {
                    alert('Login failed. Please check your credentials.');
                }
            });
        });

        logoutBtn.addEventListener('click', () => {
            fetch('/api/logout', { method: 'POST' })
                .then(() => showLogin());
        });

        // Initial check - if not authorized, show login immediately
        fetch('/api/spaces').then(res => {
            if (res.ok) {
                showApp();
            } else {
                showLogin();
            }
        }).catch(() => showLogin());

        importBtn.addEventListener('click', () => {
            const url = urlInput.value;
            const space = spaceSelect.value;
            const lang = langSelect.value;
            if (!url) return alert('Please provide a URL!');

            statusDiv.style.display = 'block';
            statusDiv.className = '';
            statusDiv.textContent = 'Starting import...';
            importBtn.disabled = true;

            fetch('/import?url=' + encodeURIComponent(url) + '&space=' + space + '&lang=' + lang)
                .then(res => {
                    if (res.status === 202) {
                        return res.json();
                    } else if (res.status === 401) {
                        statusDiv.className = 'error';
                        statusDiv.textContent = 'Session expired. Please refresh and login again.';
                        showLogin();
                        throw new Error('Unauthorized');
                    } else {
                        throw new Error('Server error');
                    }
                })
                .then(data => {
                    window.location.href = '/import/' + data.correlation_id;
                })
                .catch(err => {
                    if (err.message !== 'Unauthorized') {
                        statusDiv.className = 'error';
                        statusDiv.textContent = 'An error occurred while scheduling the import.';
                    }
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

	// Handle transcription if video is present
	if item.Transcript != "" {
		services.LogJSON(cid, "Background", "Using native transcript from scraper", "INFO")
		fullText += "\n\n--- VIDEO TRANSCRIPT ---\n" + item.Transcript
	} else if item.VideoURL != "" && h.Transcription != nil {
		services.LogJSON(cid, "Background", "Video detected, starting transcription service", "INFO")
		transcript, err := h.Transcription.TranscribeVideo(ctx, item.VideoURL, cid)
		if err != nil {
			services.LogJSON(cid, "Background", fmt.Sprintf("Transcription failed (continuing with original text): %v", err), "WARN")
		} else {
			fullText += "\n\n--- VIDEO TRANSCRIPT ---\n" + transcript
		}
	}

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

func (h *Handler) ShowImportProgress(c echo.Context) error {
    cid := c.Param("CorrelationID")
    html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Import Progress - Recipe Importer AI</title>
    <style>
        body { font-family: sans-serif; max-width: 800px; margin: 0 auto; padding: 20px; line-height: 1.6; background: #f4f7f6; }
        .container { background: white; padding: 30px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        h1 { margin-top: 0; color: #333; }
        #log-container { margin-top: 20px; background: #222; color: #eee; padding: 15px; border-radius: 4px; font-family: monospace; font-size: 12px; height: 250px; overflow-y: auto; }
        .log-entry { margin-bottom: 4px; border-bottom: 1px solid #333; padding-bottom: 2px; }
        .log-time { color: #888; margin-right: 8px; }
        .log-svc { color: #4fc3f7; font-weight: bold; margin-right: 8px; width: 80px; display: inline-block; }
        .level-ERROR { color: #ff5252; }
        .level-WARN { color: #ffb74d; }
        .level-INFO { color: #81c784; }
        
        #recipe-preview { display: none; margin-bottom: 30px; border-bottom: 2px solid #eee; padding-bottom: 30px; }
        .recipe-header { display: flex; gap: 20px; margin-bottom: 20px; }
        .recipe-image { width: 200px; height: 200px; object-fit: cover; border-radius: 8px; background: #eee; }
        .recipe-info h2 { margin: 0 0 10px 0; }
        .recipe-meta { font-size: 14px; color: #666; }
        .recipe-tags { margin-top: 10px; }
        .tag { background: #e0e0e0; padding: 2px 8px; border-radius: 12px; font-size: 12px; margin-right: 5px; display: inline-block; }
        
        .recipe-content { display: grid; grid-template-columns: 1fr 2fr; gap: 30px; }
        .ingredients ul { padding-left: 20px; }
        .steps ol { padding-left: 20px; }
        
        .actions { margin-top: 20px; display: flex; gap: 10px; }
        button { border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; font-size: 14px; transition: background 0.3s; }
        .btn-primary { background: #28a745; color: white; }
        .btn-primary:hover { background: #218838; }
        .btn-secondary { background: #6c757d; color: white; }
        .btn-secondary:hover { background: #5a6268; }
    </style>
</head>
<body>
    <div class="container">
        <div id="recipe-preview">
            <div id="recipe-data"></div>
            <div class="actions">
                <button id="viewBtn" class="btn-primary">View in Tandoor</button>
                <button onclick="window.location.href='/'" class="btn-secondary">Import Another</button>
            </div>
        </div>

        <h1>Importing Recipe...</h1>
        <p>Correlation ID: <code id="cid-val">` + cid + `</code></p>
        
        <div id="log-container"></div>
    </div>

    <script>
        const logContainer = document.getElementById('log-container');
        const recipePreview = document.getElementById('recipe-preview');
        const recipeData = document.getElementById('recipe-data');
        const viewBtn = document.getElementById('viewBtn');
        const cid = "` + cid + `";

        function addLog(entry) {
            const div = document.createElement('div');
            div.className = 'log-entry';
            const time = new Date(entry.timestamp).toLocaleTimeString();
            div.innerHTML = '<span class="log-time">' + time + '</span>' +
                            '<span class="log-svc">[' + entry.service + ']</span>' +
                            '<span class="log-msg level-' + entry.level + '">' + entry.message + '</span>';
            logContainer.appendChild(div);
            logContainer.scrollTop = logContainer.scrollHeight;
        }

        function renderRecipe(recipe) {
            recipePreview.style.display = 'block';
            document.querySelector('h1').textContent = 'Import Complete!';
            
            let ingredientsHTML = '<ul>';
            recipe.steps.forEach(step => {
                step.ingredients.forEach(ing => {
                    ingredientsHTML += '<li>' + (ing.amount || '') + ' ' + (ing.unit ? ing.unit.name : '') + ' ' + ing.food.name + '</li>';
                });
            });
            ingredientsHTML += '</ul>';

            let stepsHTML = '<ol>';
            recipe.steps.forEach(step => {
                stepsHTML += '<li><strong>' + step.name + '</strong>: ' + step.instruction + '</li>';
            });
            stepsHTML += '</ol>';

            let tagsHTML = '';
            if (recipe.keywords) {
                recipe.keywords.forEach(kw => {
                    tagsHTML += '<span class="tag">' + (kw.name || kw) + '</span>';
                });
            }

            recipeData.innerHTML = '<div class="recipe-header">' +
                    '<img src="' + (recipe.image_url || '') + '" class="recipe-image" alt="Recipe Image">' +
                    '<div class="recipe-info">' +
                        '<h2>' + recipe.name + '</h2>' +
                        '<div class="recipe-meta">' +
                            'Time: ' + (recipe.working_time + recipe.waiting_time) + ' min | Servings: ' + recipe.servings +
                        '</div>' +
                        '<div class="recipe-tags">' + tagsHTML + '</div>' +
                    '</div>' +
                '</div>' +
                '<div class="recipe-content">' +
                    '<div class="ingredients">' +
                        '<h3>Ingredients</h3>' +
                        ingredientsHTML +
                    '</div>' +
                    '<div class="steps">' +
                        '<h3>Instructions</h3>' +
                        stepsHTML +
                    '</div>' +
                '</div>';

            viewBtn.onclick = () => {
                // We assume Tandoor URL from current host or config
                // In a real scenario, you'd want the full Tandoor URL here
                window.open('/api/recipe/' + recipe.id + '/', '_blank');
            };
        }

        const logSource = new EventSource('/api/logs/' + cid);
        logSource.onmessage = (e) => {
            const entry = JSON.parse(e.data);
            if (entry.type === 'log') {
                addLog(entry);
            } else if (entry.type === 'recipe') {
                renderRecipe(entry.data);
            }
        };
        logSource.onerror = () => {
            // logSource.close();
        };
    </script>
</body>
</html>
    `
    return c.HTML(http.StatusOK, html)
}
