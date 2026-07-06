package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"recipe_importer_ai/infrastructure/api"
	"recipe_importer_ai/infrastructure/apify"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/mcp"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/usecases/auth"
	"recipe_importer_ai/usecases/book"
	"recipe_importer_ai/usecases/cookbook"
	"recipe_importer_ai/usecases/copy_space"
	"recipe_importer_ai/usecases/duplicates"
	"recipe_importer_ai/usecases/ingredient"
	"recipe_importer_ai/usecases/import_recipe"
	"recipe_importer_ai/usecases/recipe"
	"recipe_importer_ai/usecases/tag"
	"net/http"
	"strings"
	"time"

	mcp_sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/joho/godotenv"
)

func main() {
	batchFile := flag.String("file", "", "Path to txt file with URLs for batch import")
	spaceID := flag.String("space", "", "Tandoor Space ID for CLI import")
	lang := flag.String("lang", "Polish", "Target language for recipes (e.g., Polish, English, German)")
	token := flag.String("token", "", "Tandoor bearer token for CLI import")
	mcpFlag := flag.Bool("mcp", false, "Start MCP server in Stdio mode")
	flag.Parse()

	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(); err != nil {
			logger.LogJSON("system", "Main", "Error loading .env file", "WARN")
		}
	}

	ctx := context.Background()

	// 1. Initialize Infrastructure Clients
	apifyClient := apify.NewApifyService()
	geminiClient, err := gemini.NewGeminiService(ctx)
	if err != nil {
		logger.LogJSON("system", "Main", "Failed to initialize Gemini", "ERROR")
		os.Exit(1)
	}
	tandoorClient := tandoor.NewTandoorService()

	// 2. Initialize UseCases
	authUC := auth.NewAuthUseCase(tandoorClient)
	findDuplicatesUC := duplicates.NewFindUseCase(tandoorClient)
	cleanDuplicatesUC := duplicates.NewCleanUseCase(tandoorClient)

	tagsUC := cookbook.NewTagsUseCase(tandoorClient, geminiClient)
	matchUC := cookbook.NewMatchUseCase(geminiClient)
	suggestUC := cookbook.NewSuggestUseCase(tandoorClient, tagsUC, matchUC)
	addRecipesUC := cookbook.NewAddUseCase(tandoorClient)

	processor := import_recipe.NewProcessor(geminiClient)
	taskManager := import_recipe.NewTaskManager()
	importURLUC := import_recipe.NewImportURLUseCase(apifyClient, processor, tandoorClient, taskManager)
	importTextUC := import_recipe.NewImportTextUseCase(processor, tandoorClient, taskManager)
	importImageUC := import_recipe.NewImportImageUseCase(processor, tandoorClient, taskManager)

	recipeCreateUC := recipe.NewCreateUseCase(tandoorClient)
	recipeGetUC := recipe.NewGetUseCase(tandoorClient)
	recipeUpdateUC := recipe.NewUpdateUseCase(tandoorClient)
	recipeDeleteUC := recipe.NewDeleteUseCase(tandoorClient)

	tagCreateUC := tag.NewCreateUseCase(tandoorClient)
	tagGetUC := tag.NewGetUseCase(tandoorClient)
	tagUpdateUC := tag.NewUpdateUseCase(tandoorClient)
	tagDeleteUC := tag.NewDeleteUseCase(tandoorClient)

	bookCreateUC := book.NewCreateUseCase(tandoorClient)
	bookGetUC := book.NewGetUseCase(tandoorClient)
	bookUpdateUC := book.NewUpdateUseCase(tandoorClient)
	bookDeleteUC := book.NewDeleteUseCase(tandoorClient)

	ingCreateUC := ingredient.NewCreateUseCase(tandoorClient)
	ingGetUC := ingredient.NewGetUseCase(tandoorClient)
	ingUpdateUC := ingredient.NewUpdateUseCase(tandoorClient)
	ingDeleteUC := ingredient.NewDeleteUseCase(tandoorClient)

	copyTranslator := copy_space.NewTranslator(geminiClient)
	copyUC := copy_space.NewCopyUseCase(tandoorClient, copyTranslator)

	// 3. Initialize HTTP handlers
	h := api.NewApiHandler(
		tandoorClient,
		authUC,
		findDuplicatesUC,
		cleanDuplicatesUC,
		suggestUC,
		addRecipesUC,
		importURLUC,
		importTextUC,
		importImageUC,
		taskManager,
		recipeDeleteUC,
		copyUC,
	)

	// CLI Batch Mode
	if *batchFile != "" {
		if *token == "" {
			fmt.Println("Error: --token is required in CLI mode")
			os.Exit(1)
		}
		runBatchCLI(importURLUC, *batchFile, *spaceID, *token, *lang)
		return
	}

	// 4. Initialize MCP Server
	importToolsReg := func(server *mcp_sdk.Server) {
		mcp.RegisterImportTools(server, importURLUC, importTextUC, taskManager)
	}

	mcpServer := mcp.BuildMCPServer(
		tandoorClient,
		importToolsReg,
		recipeCreateUC,
		recipeGetUC,
		recipeUpdateUC,
		recipeDeleteUC,
		tagCreateUC,
		tagGetUC,
		tagUpdateUC,
		tagDeleteUC,
		bookCreateUC,
		bookGetUC,
		bookUpdateUC,
		bookDeleteUC,
		ingCreateUC,
		ingGetUC,
		ingUpdateUC,
		ingDeleteUC,
	)

	// MCP Stdio Mode
	if *mcpFlag {
		logger.LogJSON("system", "MCP", "Starting MCP server in Stdio mode", "INFO")
		if err := mcpServer.Run(context.Background(), &mcp_sdk.StdioTransport{}); err != nil {
			logger.LogJSON("system", "MCP", "Stdio MCP server failed: "+err.Error(), "ERROR")
			os.Exit(1)
		}
		return
	}

	// Server Mode
	runServer(h, mcpServer)
}

func runBatchCLI(importURLUC *import_recipe.ImportURLUseCase, filePath string, spaceID string, token string, lang string) {
	cid := fmt.Sprintf("batch-%d", time.Now().Unix())
	logger.LogJSON(cid, "CLI", fmt.Sprintf("Starting CLI batch import from %s in space %s (Target Language: %s)", filePath, spaceID, lang), "INFO")

	file, err := os.Open(filePath)
	if err != nil {
		logger.LogJSON(cid, "CLI", "Failed to open file: "+err.Error(), "ERROR")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	ctx := context.Background()
	for scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url != "" && !strings.HasPrefix(url, "#") {
			count++
			importURLUC.Execute(ctx, url, spaceID, spaceID, "CLI User", lang, false, token, cid)
		}
	}

	logger.LogJSON(cid, "CLI", fmt.Sprintf("Finished processing %d URLs sequentially", count), "INFO")
}

func runServer(h *api.ApiHandler, mcpServer *mcp_sdk.Server) {
	sseHandler := mcp_sdk.NewSSEHandler(func(req *http.Request) *mcp_sdk.Server {
		return mcpServer
	}, nil)

	e := api.SetupServer(h, sseHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	logger.LogJSON("system", "Main", "HTTP server starting on port "+port, "INFO")
	e.Logger.Fatal(e.Start(":" + port))
}
