package mcp

import (
	"context"
	"recipe_importer_ai/usecases/tag"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CreateTagArgs struct {
	Space       string  `json:"space"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type GetTagArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

type UpdateTagArgs struct {
	Space       string  `json:"space"`
	Id          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type DeleteTagArgs struct {
	Space string `json:"space"`
	Id    string `json:"id"`
}

func RegisterTagTools(
	server *mcp.Server,
	createUC *tag.CreateUseCase,
	getUC *tag.GetUseCase,
	updateUC *tag.UpdateUseCase,
	deleteUC *tag.DeleteUseCase,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_tag",
		Description: "Create a new tag in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args CreateTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "create_tag")
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
		Name:        "get_tag",
		Description: "Get details of a tag from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args GetTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "get_tag")
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
		Name:        "update_tag",
		Description: "Update a tag's name or description in Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args UpdateTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "update_tag")
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
		Name:        "delete_tag",
		Description: "Delete a tag from Tandoor.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args DeleteTagArgs) (*mcp.CallToolResult, any, error) {
		token, cid, err := getAuthAndCid(ctx, "delete_tag")
		if err != nil {
			return nil, nil, err
		}
		err = deleteUC.Execute(ctx, args.Space, args.Id, token, cid)
		if err != nil {
			return nil, nil, err
		}
		return newToolResultText("Tag successfully deleted"), nil, nil
	})
}
