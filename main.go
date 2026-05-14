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
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// Parse CLI flags
	batchFile := flag.String("file", "", "Path to txt file with URLs for batch import")
	spaceID := flag.String("space", "", "Tandoor Space ID for CLI import")
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
		Apify:   apify,
		Gemini:  gemini,
		Tandoor: tandoor,
	}

	// CLI Batch Mode
	if *batchFile != "" {
		runBatchCLI(h, *batchFile, *spaceID)
		return
	}

	// Server Mode
	runServer(h)
}

func runBatchCLI(h *api.Handler, filePath string, spaceID string) {
	cid := fmt.Sprintf("batch-%d", time.Now().Unix())
	services.LogJSON(cid, "CLI", fmt.Sprintf("Starting CLI batch import from %s in space %s", filePath, spaceID), "INFO")

	file, err := os.Open(filePath)
	if err != nil {
		services.LogJSON(cid, "CLI", "Failed to open file: "+err.Error(), "ERROR")
		return
	}
	defer file.Close()

	var wg sync.WaitGroup
	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url != "" && !strings.HasPrefix(url, "#") {
			wg.Add(1)
			count++
			go func(u string) {
				defer wg.Done()
				h.ProcessURL(u, spaceID, cid)
			}(url)
		}
	}

	services.LogJSON(cid, "CLI", fmt.Sprintf("Scheduled %d URLs, waiting for completion...", count), "INFO")
	wg.Wait()
	services.LogJSON(cid, "CLI", "Batch import finished", "INFO")
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
    e.GET("/api/spaces", h.GetSpaces)

	// API
	e.GET("/import", h.ImportRecipe)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	services.LogJSON("system", "Main", "HTTP server starting on port "+port, "INFO")
	e.Logger.Fatal(e.Start(":" + port))
}
