package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"recipe_importer_ai/services"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AuthContextFunc extracts the Tandoor token from HTTP headers and stores it in the context
func AuthContextFunc(ctx context.Context, r *http.Request) context.Context {
	token := r.Header.Get("Authorization")
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}
	if token == "" {
		token = r.Header.Get("X-Tandoor-Token")
	}
	return context.WithValue(ctx, "tandoor_token", token)
}

// getAuthAndCid extracts the token and generates a correlation ID for MCP operations
func getAuthAndCid(ctx context.Context, operation string) (string, string, error) {
	token, ok := ctx.Value("tandoor_token").(string)
	if !ok || token == "" {
		return "", "", errors.New("missing Tandoor authorization token; please provide it in the 'Authorization: Bearer <token>' or 'X-Tandoor-Token' header")
	}
	cid := fmt.Sprintf("mcp-%s-%d", operation, time.Now().UnixNano())
	return token, cid, nil
}

// NewMCPServer initializes the MCP server and registers the recipe management tools
func NewMCPServer(h *Handler) *server.SSEServer {
	// 1. Create the base MCP server
	s := server.NewMCPServer("Recipe Importer AI", "1.0.0")

	// 2. list_spaces tool
	listSpacesTool := mcp.NewTool("list_spaces",
		mcp.WithDescription("Get a list of all recipe spaces in Tandoor. This is needed to get the Space ID to import recipes into."),
	)
	s.AddTool(listSpacesTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token, cid, err := getAuthAndCid(ctx, "spaces")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		spaces, err := h.Tandoor.GetSpaces(token, cid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to get spaces: %v", err)), nil
		}

		var sb strings.Builder
		sb.WriteString("Available Tandoor Spaces:\n")
		for _, space := range spaces {
			sb.WriteString(fmt.Sprintf("- ID: %d, Name: %s\n", space.ID, space.Name))
		}
		return mcp.NewToolResultText(sb.String()), nil
	})

	// 3. import_recipe_from_url tool
	importUrlTool := mcp.NewTool("import_recipe_from_url",
		mcp.WithDescription("Import a recipe into Tandoor from a URL (e.g. YouTube, Instagram, Facebook post/page, or general blogs/websites). This is done asynchronously."),
		mcp.WithString("url", mcp.Required(), mcp.Description("The recipe source URL")),
		mcp.WithString("space_id", mcp.Description("The Tandoor Space ID to import the recipe into. If not specified, the first available space or default will be used.")),
		mcp.WithString("lang", mcp.Description("Target language for translation/formatting. Default: 'Polish'")),
	)
	s.AddTool(importUrlTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token, cid, err := getAuthAndCid(ctx, "url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		url, err := req.RequireString("url")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		args, _ := req.Params.Arguments.(map[string]any)

		var spaceID string
		if args != nil {
			if val, exists := args["space_id"]; exists {
				spaceID, _ = val.(string)
			}
		}

		lang := "Polish"
		if args != nil {
			if val, exists := args["lang"]; exists {
				if l, ok := val.(string); ok && l != "" {
					lang = l
				}
			}
		}

		username := "MCP Client"
		spaceName := h.resolveSpaceName(spaceID, token, cid)

		services.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP import request for URL: %s in space %s (Lang: %s)", url, spaceName, lang), "INFO")

		go h.ProcessURL(url, spaceID, spaceName, username, lang, false, token, cid)

		return mcp.NewToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil
	})

	// 4. import_recipe_from_text tool
	importTextTool := mcp.NewTool("import_recipe_from_text",
		mcp.WithDescription("Extract a recipe from raw text (e.g. user-provided recipe content) using Gemini and import it into Tandoor. This is done asynchronously."),
		mcp.WithString("text", mcp.Required(), mcp.Description("The raw recipe text (ingredients, steps, preparation)")),
		mcp.WithString("space_id", mcp.Description("The Tandoor Space ID to import the recipe into.")),
		mcp.WithString("lang", mcp.Description("Target language for translation/formatting. Default: 'Polish'")),
	)
	s.AddTool(importTextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token, cid, err := getAuthAndCid(ctx, "text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text, err := req.RequireString("text")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		args, _ := req.Params.Arguments.(map[string]any)

		var spaceID string
		if args != nil {
			if val, exists := args["space_id"]; exists {
				spaceID, _ = val.(string)
			}
		}

		lang := "Polish"
		if args != nil {
			if val, exists := args["lang"]; exists {
				if l, ok := val.(string); ok && l != "" {
					lang = l
				}
			}
		}

		username := "MCP Client"
		spaceName := h.resolveSpaceName(spaceID, token, cid)

		services.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP text import request in space %s (Lang: %s)", spaceName, lang), "INFO")

		go h.ProcessText(text, spaceID, spaceName, username, lang, false, token, cid)

		return mcp.NewToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil
	})

	// 5. get_import_status tool
	getStatusTool := mcp.NewTool("get_import_status",
		mcp.WithDescription("Check the status and associated log history of an ongoing or completed recipe import task using its correlation ID."),
		mcp.WithString("correlation_id", mcp.Required(), mcp.Description("The correlation ID returned when starting the import")),
	)
	s.AddTool(getStatusTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cid, err := req.RequireString("correlation_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		h.importsMu.Lock()
		var foundTask *ImportTask
		for _, imp := range h.imports {
			if imp.CorrelationID == cid {
				foundTask = imp
				break
			}
		}
		h.importsMu.Unlock()

		if foundTask == nil {
			return mcp.NewToolResultText(fmt.Sprintf("No import task found with correlation ID: %s", cid)), nil
		}

		// Retrieve associated logs from our in-memory logger buffer
		logs := services.GetLogsForCorrelationID(cid)
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Task URL: %s\n", foundTask.URL))
		sb.WriteString(fmt.Sprintf("Status: %s\n", foundTask.Status))
		sb.WriteString(fmt.Sprintf("Created At: %s\n", foundTask.CreatedAt.Format(time.RFC3339)))
		sb.WriteString(fmt.Sprintf("Space: %s\n", foundTask.Space))
		sb.WriteString("\nAssociated Logs:\n")
		if len(logs) == 0 {
			sb.WriteString("(No logs recorded yet)\n")
		} else {
			for _, entry := range logs {
				sb.WriteString(fmt.Sprintf("[%s] [%s] %s\n", entry.Timestamp, entry.Level, entry.Message))
			}
		}

		return mcp.NewToolResultText(sb.String()), nil
	})

	// 6. delete_recipe tool
	deleteRecipeTool := mcp.NewTool("delete_recipe",
		mcp.WithDescription("Delete a recipe from Tandoor by its recipe ID."),
		mcp.WithString("id", mcp.Required(), mcp.Description("The ID of the recipe to delete")),
	)
	s.AddTool(deleteRecipeTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		token, cid, err := getAuthAndCid(ctx, "delete")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		id, err := req.RequireString("id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		err = h.Tandoor.DeleteRecipe(id, token, cid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to delete recipe: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Recipe %s deleted successfully.", id)), nil
	})

	// 7. Initialize and return the SSE wrapper
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:" + port
	}

	sseServer := server.NewSSEServer(s,
		server.WithBaseURL(baseURL),
		server.WithSSEEndpoint("/sse"),
		server.WithMessageEndpoint("/message"),
		server.WithSSEContextFunc(AuthContextFunc),
	)

	return sseServer
}
