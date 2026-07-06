package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"recipe_importer_ai/api"
	"recipe_importer_ai/services"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Parse CLI flags
	batchFile := flag.String("file", "", "Path to txt file with URLs for batch import")
	spaceID := flag.String("space", "", "Tandoor Space ID for CLI import")
	lang := flag.String("lang", "Polish", "Target language for recipes (e.g., Polish, English, German)")
	token := flag.String("token", "", "Tandoor bearer token for CLI import")
	mcpFlag := flag.Bool("mcp", false, "Start MCP server in Stdio mode")
	flag.Parse()

	// Load .env file
	if err := godotenv.Load(); err != nil {
		services.LogJSON("system", "Main", "Warning: .env file not found", "WARN")
	}

	ctx := context.Background()

	// Initialize services
	apify := services.NewApifyService()
	gemini, err := services.NewGeminiService(ctx)
	if err != nil {
		services.LogJSON("system", "Main", "Failed to initialize Gemini", "ERROR")
		os.Exit(1)
	}
	tandoor := services.NewTandoorService()


	// Initialize handler/processor
	h := &api.Handler{
		Apify:         apify,
		Gemini:        gemini,
		Tandoor:       tandoor,
	}

	// CLI Batch Mode
	if *batchFile != "" {
		if *token == "" {
			fmt.Println("Error: --token is required in CLI mode")
			os.Exit(1)
		}
		runBatchCLI(h, *batchFile, *spaceID, *token, *lang)
		return
	}

	// MCP Stdio Mode
	if *mcpFlag {
		mcpServer := api.BuildMCPServer(h)
		services.LogJSON("system", "MCP", "Starting MCP server in Stdio mode", "INFO")
		if err := mcpServer.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			services.LogJSON("system", "MCP", "Stdio MCP server failed: "+err.Error(), "ERROR")
			os.Exit(1)
		}
		return
	}

	// Server Mode
	runServer(h)
}

func runBatchCLI(h *api.Handler, filePath string, spaceID string, token string, lang string) {
	cid := fmt.Sprintf("batch-%d", time.Now().Unix())
	services.LogJSON(cid, "CLI", fmt.Sprintf("Starting CLI batch import from %s in space %s (Target Language: %s)", filePath, spaceID, lang), "INFO")

	file, err := os.Open(filePath)
	if err != nil {
		services.LogJSON(cid, "CLI", "Failed to open file: "+err.Error(), "ERROR")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url != "" && !strings.HasPrefix(url, "#") {
			count++
			h.ProcessURL(url, spaceID, spaceID, "CLI User", lang, false, token, cid)
		}
	}

	services.LogJSON(cid, "CLI", fmt.Sprintf("Finished processing %d URLs sequentially", count), "INFO")
}

func runServer(h *api.Handler) {
	e := echo.New()

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
		Format: `{"time":"${time_rfc3339_nano}","correlation-id":"${header:X-Correlation-ID}","remote_ip":"${remote_ip}",` +
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
    e.GET("/api/tools/duplicates", h.GetDuplicates)
    e.POST("/api/tools/clean-duplicates", h.CleanDuplicates)
    e.POST("/api/tools/suggest-book-recipes", h.SuggestBookRecipes)
    e.POST("/api/tools/add-recipes-to-book", h.AddRecipesToBook)

	// MCP Server (SSE Mode)
	mcpServer := api.NewMCPServer(h)
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

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	services.LogJSON("system", "Main", "HTTP server starting on port "+port, "INFO")
	e.Logger.Fatal(e.Start(":" + port))
}
