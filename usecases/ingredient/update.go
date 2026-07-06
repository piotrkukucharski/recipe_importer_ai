package ingredient

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/tandoor"
)

type UpdateUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewUpdateUseCase(t *tandoor.TandoorService) *UpdateUseCase {
	return &UpdateUseCase{Tandoor: t}
}

func (uc *UpdateUseCase) Execute(ctx context.Context, spaceID string, id string, name *string, description *string, token string, cid string) (map[string]interface{}, error) {
	body := make(map[string]interface{})
	if name != nil {
		body["name"] = *name
	}
	if description != nil {
		body["description"] = *description
	}
	path := fmt.Sprintf("/api/food/%s/", id)
	return uc.Tandoor.PatchWithRetry(path, body, spaceID, token, cid)
}
