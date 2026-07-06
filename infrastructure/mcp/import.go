package mcp

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/usecases/import_recipe"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ImportRecipeFromURLArgs struct {
	Url   string  `json:"url"`
	Space string  `json:"space"`
	Lang  *string `json:"lang,omitempty"`
}

type ImportRecipeFromTextArgs struct {
	Text  string  `json:"text"`
	Space string  `json:"space"`
	Lang  *string `json:"lang,omitempty"`
}

type GetImportStatusArgs struct {
	CorrelationID string `json:"correlation_id"`
	Space         string `json:"space"`
}

func RegisterImportTools(
	server *mcp.Server,
	importURLUC *import_recipe.ImportURLUseCase,
	importTextUC *import_recipe.ImportTextUseCase,
	taskManager *import_recipe.TaskManager,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "import_recipe_from_url",
		Description: "Import a recipe into Tandoor from a URL (e.g. YouTube, Instagram, Facebook post/page, or general blogs/websites) asynchronously.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImportRecipeFromURLArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "url")
		if err != nil {
			return nil, nil, err
		}

		lang := "Polish"
		if args.Lang != nil && *args.Lang != "" {
			lang = *args.Lang
		}

		username := "MCP Client"
		spaceName := args.Space

		logger.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP import request for URL: %s in space %s (Lang: %s)", args.Url, spaceName, lang), "INFO")

		go importURLUC.Execute(context.Background(), args.Url, args.Space, spaceName, username, lang, false, token, cid)

		return newToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "import_recipe_from_text",
		Description: "Extract a recipe from raw text (ingredients, steps, preparation) using Gemini and import it into Tandoor asynchronously.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ImportRecipeFromTextArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "text")
		if err != nil {
			return nil, nil, err
		}

		lang := "Polish"
		if args.Lang != nil && *args.Lang != "" {
			lang = *args.Lang
		}

		username := "MCP Client"
		spaceName := args.Space

		logger.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP text import request in space %s (Lang: %s)", spaceName, lang), "INFO")

		go importTextUC.Execute(context.Background(), args.Text, args.Space, spaceName, username, lang, false, token, cid)

		return newToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_import_status",
		Description: "Check the status and log history of an ongoing or completed recipe import task using its correlation ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetImportStatusArgs) (*mcp.CallToolResult, any, error) {
		foundTask := taskManager.GetTask(args.CorrelationID)
		if foundTask == nil {
			return newToolResultText(fmt.Sprintf("No import task found with correlation ID: %s", args.CorrelationID)), nil, nil
		}

		logs := logger.GetLogsForCorrelationID(args.CorrelationID)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Task URL: %s\n", foundTask.URL))
		sb.WriteString(fmt.Sprintf("Status: %s\n", foundTask.Status))
		sb.WriteString(fmt.Sprintf("Created At: %s\n", foundTask.CreatedAt.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("User: %s\n", foundTask.User))
		sb.WriteString(fmt.Sprintf("Space: %s\n\n", foundTask.Space))
		sb.WriteString("Log History:\n")
		for _, log := range logs {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", log.Timestamp, log.Message))
		}

		return newToolResultText(sb.String()), nil, nil
	})
}
