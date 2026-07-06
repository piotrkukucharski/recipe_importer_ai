package mcp

import (
	"context"
	"recipe_importer_ai/usecases/book"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateBookArgs struct {
	Space       string  `json:"space"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type GetBookArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

type UpdateBookArgs struct {
	Space       string  `json:"space"`
	Id          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type DeleteBookArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

func RegisterBookTools(
	server *mcp.Server,
	createUC *book.CreateUseCase,
	getUC *book.GetUseCase,
	updateUC *book.UpdateUseCase,
	deleteUC *book.DeleteUseCase,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_book",
		Description: "Create a new recipe book in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_book")
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
		Name:        "get_book",
		Description: "Get details of a recipe book from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_book")
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
		Name:        "update_book",
		Description: "Update a recipe book's name or description in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_book")
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
		Name:        "delete_book",
		Description: "Delete a recipe book from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteBookArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_book")
		if err != nil {
			return nil, nil, err
		}
		err = deleteUC.Execute(ctx, args.Space, args.Id, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText("Recipe book successfully deleted"), nil, nil
	})
}
