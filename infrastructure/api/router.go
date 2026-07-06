package api

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/web"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func SetupServer(h *ApiHandler, mcpServer *mcp.SSEHandler) *echo.Echo {
	e := echo.New()

	e.Renderer = web.NewTemplateRenderer()

	// Middleware Correlation ID
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rid := c.Request().Header.Get("X-Correlation-ID")
			if rid == "" {
				rid = fmt.Sprintf("%d", time.Now().UnixNano())
				c.Request().Header.Set("X-Correlation-ID", rid)
			}
			c.Response().Header().Set("X-Correlation-ID", rid)
			return next(c)
		}
	})

	// Logger JSON
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: `{"timestamp":"${time_rfc3339}","correlation-id":"${header:X-Correlation-ID}","remote_ip":"${remote_ip}",` +
			`"host":"${host}","method":"${method}","uri":"${uri}","status":${status},` +
			`"latency_human":"${latency_human}","bytes_out":${bytes_out}}` + "\n",
	}))

	e.Use(middleware.Recover())

	// Web UI
	e.GET("/", h.ShowIndex)
	e.GET("/imports", h.ShowImports)
	e.GET("/tools", h.ShowTools)
	e.GET("/api/spaces", h.GetSpaces)
	e.POST("/api/login", h.Login)
	e.POST("/api/logout", h.Logout)
	e.GET("/api/logs", h.GetLogs)
	e.GET("/api/logs/:CorrelationID", h.GetLogsByCorrelationID)

	// API
	e.GET("/import", h.ImportRecipe)
	e.POST("/import-text", h.ImportRecipeFromText)
	e.POST("/import-images", h.ImportRecipeFromImages)
	e.POST("/import-custom", h.ImportRecipeCustom)
	e.GET("/import/:CorrelationID", h.ShowImportProgress)
	e.DELETE("/api/recipe/:id", h.DeleteRecipe)

	// Tools API
	e.GET("/api/tools/books", h.GetRecipeBooks)
	e.GET("/api/tools/space-recipes", h.GetSpaceRecipes)
	e.GET("/api/tools/copy-space/stream", h.CopySpaceStream)
	e.GET("/api/tools/cleanup/stream", h.CleanupStream)
	e.GET("/api/tools/duplicates", h.GetDuplicates)
	e.POST("/api/tools/clean-duplicates", h.CleanDuplicates)
	e.GET("/api/tools/suggest-book-recipes/stream", h.SuggestBookRecipesStream)
	e.POST("/api/tools/add-recipes-to-book", h.AddRecipesToBook)

	// MCP Server (SSE Mode)
	e.Any("/sse", func(c echo.Context) error {
		req := c.Request()
		token := req.Header.Get("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.TrimPrefix(token, "Bearer ")
		}
		if token == "" {
			token = req.Header.Get("X-Tandoor-Token")
		}
		if token == "" {
			token = req.URL.Query().Get("token")
		}

		ctx := context.WithValue(req.Context(), "tandoor_token", token)
		c.SetRequest(req.WithContext(ctx))

		mcpServer.ServeHTTP(c.Response().Writer, c.Request())
		return nil
	})

	return e
}
