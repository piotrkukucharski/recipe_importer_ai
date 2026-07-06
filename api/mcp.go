package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"recipe_importer_ai/services"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// getAuthAndCid extracts the token and generates a correlation ID for MCP operations
func getAuthAndCid(ctx context.Context, operation string) (string, string, error) {
	token, ok := ctx.Value("tandoor_token").(string)
	if !ok || token == "" {
		return "", "", errors.New("missing Tandoor authorization token; please provide it in the 'Authorization: Bearer <token>', 'X-Tandoor-Token' header, or '?token=' query parameter")
	}
	cid := fmt.Sprintf("mcp-%s-%d", operation, time.Now().UnixNano())
	return token, cid, nil
}

func newToolResultText(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: text,
			},
		},
	}
}

func newToolResultJSON(data interface{}) *mcp.CallToolResult {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return newToolResultText(fmt.Sprintf("Failed to format output: %v", err))
	}
	return newToolResultText(string(bytes))
}

// Structs for tool arguments

type ListSpacesArgs struct{}

type ChangeSpaceArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID to switch to/validate"`
}

type ImportRecipeFromURLArgs struct {
	Url   string  `json:"url" jsonschema:"description=The recipe source URL"`
	Space string  `json:"space" jsonschema:"description=The Tandoor Space ID to import the recipe into"`
	Lang  *string `json:"lang,omitempty" jsonschema:"description=Target language for translation/formatting. Default: 'Polish'"`
}

type ImportRecipeFromTextArgs struct {
	Text  string  `json:"text" jsonschema:"description=The raw recipe text (ingredients, steps, preparation)"`
	Space string  `json:"space" jsonschema:"description=The Tandoor Space ID to import the recipe into"`
	Lang  *string `json:"lang,omitempty" jsonschema:"description=Target language for translation/formatting. Default: 'Polish'"`
}

type GetImportStatusArgs struct {
	CorrelationID string `json:"correlation_id" jsonschema:"description=The correlation ID returned when starting the import"`
	Space         string `json:"space" jsonschema:"description=The Tandoor Space ID"`
}

type CreateRecipeArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Name        string  `json:"name" jsonschema:"description=The recipe name"`
	Description *string `json:"description,omitempty" jsonschema:"description=The recipe description"`
}

type GetRecipeArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the recipe to get"`
}

type UpdateRecipeArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id          string  `json:"id" jsonschema:"description=The ID of the recipe to update"`
	Name        *string `json:"name,omitempty" jsonschema:"description=New recipe name"`
	Description *string `json:"description,omitempty" jsonschema:"description=New recipe description"`
}

type DeleteRecipeArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the recipe to delete"`
}

type CreateTagArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Name        string  `json:"name" jsonschema:"description=The tag name"`
	Description *string `json:"description,omitempty" jsonschema:"description=The tag description"`
}

type GetTagArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the tag to get"`
}

type UpdateTagArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id          string  `json:"id" jsonschema:"description=The ID of the tag to update"`
	Name        *string `json:"name,omitempty" jsonschema:"description=New tag name"`
	Description *string `json:"description,omitempty" jsonschema:"description=New tag description"`
}

type DeleteTagArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the tag to delete"`
}

type CreateBookArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Name        string  `json:"name" jsonschema:"description=The recipe book name"`
	Description *string `json:"description,omitempty" jsonschema:"description=The book description"`
}

type GetBookArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the book to get"`
}

type UpdateBookArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id          string  `json:"id" jsonschema:"description=The ID of the book to update"`
	Name        *string `json:"name,omitempty" jsonschema:"description=New book name"`
	Description *string `json:"description,omitempty" jsonschema:"description=New book description"`
}

type DeleteBookArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the book to delete"`
}

type CreateIngredientArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Name        string  `json:"name" jsonschema:"description=The ingredient name"`
	Description *string `json:"description,omitempty" jsonschema:"description=The ingredient description"`
}

type GetIngredientArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the ingredient to get"`
}

type UpdateIngredientArgs struct {
	Space       string  `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id          string  `json:"id" jsonschema:"description=The ID of the ingredient to update"`
	Name        *string `json:"name,omitempty" jsonschema:"description=New ingredient name"`
	Description *string `json:"description,omitempty" jsonschema:"description=New ingredient description"`
}

type DeleteIngredientArgs struct {
	Space string `json:"space" jsonschema:"description=The Tandoor Space ID"`
	Id    string `json:"id" jsonschema:"description=The ID of the ingredient to delete"`
}

// NewMCPServer initializes the MCP server and registers the recipe management tools
func NewMCPServer(h *Handler) *mcp.SSEHandler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "Recipe Importer AI",
		Version: "1.0.0",
	}, nil)

	// 1. list_spaces
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_spaces",
		Description: "Get a list of all recipe spaces in Tandoor. This is needed to get the Space ID to import recipes into.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ListSpacesArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "spaces")
		if err != nil {
			return nil, nil, err
		}

		spaces, err := h.Tandoor.GetSpaces(token, cid)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get spaces: %v", err)
		}

		var sb strings.Builder
		sb.WriteString("Available Tandoor Spaces:\n")
		for _, space := range spaces {
			sb.WriteString(fmt.Sprintf("- ID: %d, Name: %s\n", space.ID, space.Name))
		}
		return newToolResultText(sb.String()), nil, nil
	})

	// 2. change_space
	mcp.AddTool(server, &mcp.Tool{
		Name:        "change_space",
		Description: "Validate and switch context to a specific Tandoor Space ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ChangeSpaceArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "change_space")
		if err != nil {
			return nil, nil, err
		}

		spaces, err := h.Tandoor.GetSpaces(token, cid)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get spaces: %v", err)
		}

		found := false
		var spaceName string
		for _, s := range spaces {
			if fmt.Sprintf("%d", s.ID) == args.Space || s.Name == args.Space {
				found = true
				spaceName = s.Name
				break
			}
		}

		if !found {
			return nil, nil, fmt.Errorf("space '%s' not found. Use list_spaces to see available spaces", args.Space)
		}

		return newToolResultText(fmt.Sprintf("Successfully validated and switched to space: %s (ID: %s)", spaceName, args.Space)), nil, nil
	})

	// 3. import_recipe_from_url
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
		spaceName := h.resolveSpaceName(args.Space, token, cid)

		services.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP import request for URL: %s in space %s (Lang: %s)", args.Url, spaceName, lang), "INFO")

		go h.ProcessURL(args.Url, args.Space, spaceName, username, lang, false, token, cid)

		return newToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil, nil
	})

	// 4. import_recipe_from_text
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
		spaceName := h.resolveSpaceName(args.Space, token, cid)

		services.LogJSON(cid, "MCP", fmt.Sprintf("Received MCP text import request in space %s (Lang: %s)", spaceName, lang), "INFO")

		go h.ProcessText(args.Text, args.Space, spaceName, username, lang, false, token, cid)

		return newToolResultText(fmt.Sprintf("Import started. Correlation ID: %s. Use get_import_status to monitor progress.", cid)), nil, nil
	})

	// 5. get_import_status
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_import_status",
		Description: "Check the status and log history of an ongoing or completed recipe import task using its correlation ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetImportStatusArgs) (*mcp.CallToolResult, any, error) {
		h.importsMu.Lock()
		var foundTask *ImportTask
		for _, imp := range h.imports {
			if imp.CorrelationID == args.CorrelationID {
				foundTask = imp
				break
			}
		}
		h.importsMu.Unlock()

		if foundTask == nil {
			return newToolResultText(fmt.Sprintf("No import task found with correlation ID: %s", args.CorrelationID)), nil, nil
		}

		logs := services.GetLogsForCorrelationID(args.CorrelationID)
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

		return newToolResultText(sb.String()), nil, nil
	})

	// === RECIPES CRUD ===
	// Create Recipe
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_recipe",
		Description: "Create a new recipe in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_recipe")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{
			"name": args.Name,
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PostWithRetry("/api/recipe/", body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Get Recipe
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_recipe",
		Description: "Get details of a recipe from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_recipe")
		if err != nil {
			return nil, nil, err
		}

		res, err := h.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/recipe/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Update Recipe
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_recipe",
		Description: "Update a recipe in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_recipe")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{}
		if args.Name != nil {
			body["name"] = *args.Name
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PatchWithRetry(fmt.Sprintf("/api/recipe/%s/", args.Id), body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Delete Recipe
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_recipe",
		Description: "Delete a recipe from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_recipe")
		if err != nil {
			return nil, nil, err
		}

		err = h.Tandoor.DeleteWithRetry(fmt.Sprintf("/api/recipe/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText(fmt.Sprintf("Recipe %s deleted successfully.", args.Id)), nil, nil
	})

	// === TAGS (KEYWORDS) CRUD ===
	// Create Tag
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_tag",
		Description: "Create a new tag (keyword) in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_tag")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{
			"name": args.Name,
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PostWithRetry("/api/keyword/", body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Get Tag
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_tag",
		Description: "Get tag details from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_tag")
		if err != nil {
			return nil, nil, err
		}

		res, err := h.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/keyword/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Update Tag
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_tag",
		Description: "Update a tag in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_tag")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{}
		if args.Name != nil {
			body["name"] = *args.Name
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PatchWithRetry(fmt.Sprintf("/api/keyword/%s/", args.Id), body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Delete Tag
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_tag",
		Description: "Delete a tag from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_tag")
		if err != nil {
			return nil, nil, err
		}

		err = h.Tandoor.DeleteWithRetry(fmt.Sprintf("/api/keyword/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText(fmt.Sprintf("Tag %s deleted successfully.", args.Id)), nil, nil
	})

	// === BOOKS CRUD ===
	// Create Book
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_book",
		Description: "Create a new recipe book in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_book")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{
			"name":   args.Name,
			"shared": []interface{}{},
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PostWithRetry("/api/recipe-book/", body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Get Book
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_book",
		Description: "Get recipe book details from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_book")
		if err != nil {
			return nil, nil, err
		}

		res, err := h.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/recipe-book/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Update Book
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_book",
		Description: "Update a recipe book in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_book")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{}
		if args.Name != nil {
			body["name"] = *args.Name
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PatchWithRetry(fmt.Sprintf("/api/recipe-book/%s/", args.Id), body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Delete Book
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_book",
		Description: "Delete a recipe book from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_book")
		if err != nil {
			return nil, nil, err
		}

		err = h.Tandoor.DeleteWithRetry(fmt.Sprintf("/api/recipe-book/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText(fmt.Sprintf("Recipe book %s deleted successfully.", args.Id)), nil, nil
	})

	// === INGREDIENTS (FOODS) CRUD ===
	// Create Ingredient
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_ingredient",
		Description: "Create a new ingredient (food item) in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_ingredient")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{
			"name": args.Name,
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PostWithRetry("/api/food/", body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Get Ingredient
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ingredient",
		Description: "Get ingredient details from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_ingredient")
		if err != nil {
			return nil, nil, err
		}

		res, err := h.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/food/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Update Ingredient
	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_ingredient",
		Description: "Update an ingredient in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_ingredient")
		if err != nil {
			return nil, nil, err
		}

		body := map[string]interface{}{}
		if args.Name != nil {
			body["name"] = *args.Name
		}
		if args.Description != nil {
			body["description"] = *args.Description
		}

		res, err := h.Tandoor.PatchWithRetry(fmt.Sprintf("/api/food/%s/", args.Id), body, args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	// Delete Ingredient
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_ingredient",
		Description: "Delete an ingredient from Tandoor by ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_ingredient")
		if err != nil {
			return nil, nil, err
		}

		err = h.Tandoor.DeleteWithRetry(fmt.Sprintf("/api/food/%s/", args.Id), args.Space, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText(fmt.Sprintf("Ingredient %s deleted successfully.", args.Id)), nil, nil
	})

	// Create the SSE handler that exposes the server
	sseHandler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return server
	}, nil)

	return sseHandler
}
