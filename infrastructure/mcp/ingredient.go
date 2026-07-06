package mcp

import (
	"context"
	"recipe_importer_ai/usecases/ingredient"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateIngredientArgs struct {
	Space       string  `json:"space"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type GetIngredientArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

type UpdateIngredientArgs struct {
	Space       string  `json:"space"`
	Id          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type DeleteIngredientArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

func RegisterIngredientTools(
	server *mcp.Server,
	createUC *ingredient.CreateUseCase,
	getUC *ingredient.GetUseCase,
	updateUC *ingredient.UpdateUseCase,
	deleteUC *ingredient.DeleteUseCase,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_ingredient",
		Description: "Create a new ingredient in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_ingredient")
		if err != nil {
			return nil, nil, err
		}
		res, err := createUC.Execute(ctx, args.Space, args.Name, args.Description, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_ingredient",
		Description: "Get details of an ingredient from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_ingredient")
		if err != nil {
			return nil, nil, err
		}
		res, err := getUC.Execute(ctx, args.Space, args.Id, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_ingredient",
		Description: "Update an ingredient's name or description in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_ingredient")
		if err != nil {
			return nil, nil, err
		}
		res, err := updateUC.Execute(ctx, args.Space, args.Id, args.Name, args.Description, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultJSON(res), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_ingredient",
		Description: "Delete an ingredient from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteIngredientArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_ingredient")
		if err != nil {
			return nil, nil, err
		}
		err = deleteUC.Execute(ctx, args.Space, args.Id, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText("Ingredient successfully deleted"), nil, nil
	})
}
