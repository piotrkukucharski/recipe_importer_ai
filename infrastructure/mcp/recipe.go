package mcp

import (
	"context"
	"recipe_importer_ai/usecases/recipe"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateRecipeArgs struct {
	Space       string  `json:"space"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type GetRecipeArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

type UpdateRecipeArgs struct {
	Space       string  `json:"space"`
	Id          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type DeleteRecipeArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

func RegisterRecipeTools(
	server *mcp.Server,
	createUC *recipe.CreateUseCase,
	getUC *recipe.GetUseCase,
	updateUC *recipe.UpdateUseCase,
	deleteUC *recipe.DeleteUseCase,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_recipe",
		Description: "Create a new recipe in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_recipe")
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
		Name:        "get_recipe",
		Description: "Get details of a recipe from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_recipe")
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
		Name:        "update_recipe",
		Description: "Update a recipe's name or description in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_recipe")
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
		Name:        "delete_recipe",
		Description: "Delete a recipe from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteRecipeArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_recipe")
		if err != nil {
			return nil, nil, err
		}
		err = deleteUC.Execute(ctx, args.Space, args.Id, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText("Recipe successfully deleted"), nil, nil
	})
}
