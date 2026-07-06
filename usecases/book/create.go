package book

import (
	"context"
	"recipe_importer_ai/infrastructure/tandoor"
)

type CreateUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewCreateUseCase(t *tandoor.TandoorService) *CreateUseCase {
	return &CreateUseCase{Tandoor: t}
}

func (uc *CreateUseCase) Execute(ctx context.Context, spaceID string, name string, description *string, token string, cid string) (map[string]interface{}, error) {
	body := map[string]interface{}{"name": name}
	if description != nil {
		body["description"] = *description
	}
	return uc.Tandoor.PostWithRetry("/api/recipe-book/", body, spaceID, token, cid)
}
