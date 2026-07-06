package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/usecases/book"
	"recipe_importer_ai/usecases/ingredient"
	"recipe_importer_ai/usecases/recipe"
	"recipe_importer_ai/usecases/tag"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func getAuthAndCid(ctx context.Context, operation string) (string, string, error) {
	token, ok := ctx.Value("tandoor_token").(string)
	if !ok || token == "" {
		token = os.Getenv("TANDOOR_BEARER_TOKEN")
	}
	if token == "" {
		return "", "", errors.New("missing Tandoor authorization token; please provide it in request headers/query (SSE mode) or set TANDOOR_BEARER_TOKEN environment variable (Stdio mode)")
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

type ListSpacesArgs struct{}

type ChangeSpaceArgs struct {
	Space string `json:"space" jsonschema:"The Tandoor Space ID to switch to/validate"`
}

func BuildMCPServer(
	t *tandoor.TandoorService,
	importToolsReg func(*mcp.Server),
	recipeCreate *recipe.CreateUseCase,
	recipeGet *recipe.GetUseCase,
	recipeUpdate *recipe.UpdateUseCase,
	recipeDelete *recipe.DeleteUseCase,
	tagCreate *tag.CreateUseCase,
	tagGet *tag.GetUseCase,
	tagUpdate *tag.UpdateUseCase,
	tagDelete *tag.DeleteUseCase,
	bookCreate *book.CreateUseCase,
	bookGet *book.GetUseCase,
	bookUpdate *book.UpdateUseCase,
	bookDelete *book.DeleteUseCase,
	ingCreate *ingredient.CreateUseCase,
	ingGet *ingredient.GetUseCase,
	ingUpdate *ingredient.UpdateUseCase,
	ingDelete *ingredient.DeleteUseCase,
) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "Recipe Importer AI",
		Version: "1.0.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_spaces",
		Description: "Get a list of all recipe spaces in Tandoor. This is needed to get the Space ID to import recipes into.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ListSpacesArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "spaces")
		if err != nil {
			return nil, nil, err
		}

		spaces, err := t.GetSpaces(token, cid)
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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "change_space",
		Description: "Validate and switch context to a specific Tandoor Space ID.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args ChangeSpaceArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "change_space")
		if err != nil {
			return nil, nil, err
		}

		spaces, err := t.GetSpaces(token, cid)
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

	importToolsReg(server)
	RegisterRecipeTools(server, recipeCreate, recipeGet, recipeUpdate, recipeDelete)
	RegisterTagTools(server, tagCreate, tagGet, tagUpdate, tagDelete)
	RegisterBookTools(server, bookCreate, bookGet, bookUpdate, bookDelete)
	RegisterIngredientTools(server, ingCreate, ingGet, ingUpdate, ingDelete)

	return server
}
