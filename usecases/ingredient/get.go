package ingredient

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/tandoor"
)

type GetUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewGetUseCase(t *tandoor.TandoorService) *GetUseCase {
	return &GetUseCase{Tandoor: t}
}

func (uc *GetUseCase) Execute(ctx context.Context, spaceID string, id string, token string, cid string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/food/%s/", id)
	return uc.Tandoor.GetSingleWithRetry(path, spaceID, token, cid)
}
