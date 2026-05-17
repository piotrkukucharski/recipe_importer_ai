package api

import (
	"context"
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
                        statusDiv.className = 'success';
                        statusDiv.textContent = 'Import task submitted! It is running in the background. Check Tandoor in a moment.';
                        urlInput.value = '';
                    } else if (res.status === 401) {
                        statusDiv.className = 'error';
                        statusDiv.textContent = 'Session expired. Please refresh and login again.';
                        showLogin();
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
    // If AI picked an image, it's already in recipe.ImageURL
    // If it didn't pick anything, we fall back to the first one found by scraper if available
    if recipe.ImageURL == "" && item.ImageURL != "" {
        recipe.ImageURL = item.ImageURL
    }

	if err := h.Tandoor.SaveRecipe(recipe, spaceID, token, cid); err != nil {
		services.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for %s: %v", item.URL, err), "ERROR")
		return
	}

	services.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
}
