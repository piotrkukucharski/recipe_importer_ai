package ingredient

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/tandoor"
)

type DeleteUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewDeleteUseCase(t *tandoor.TandoorService) *DeleteUseCase {
	return &DeleteUseCase{Tandoor: t}
}

func (uc *DeleteUseCase) Execute(ctx context.Context, spaceID string, id string, token string, cid string) error {
	path := fmt.Sprintf("/api/food/%s/", id)
	return uc.Tandoor.DeleteWithRetry(path, spaceID, token, cid)
}
