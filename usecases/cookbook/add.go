package cookbook

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
)

type AddUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewAddUseCase(t *tandoor.TandoorService) *AddUseCase {
	return &AddUseCase{Tandoor: t}
}

func (uc *AddUseCase) Execute(ctx context.Context, bookID int, recipeIDs []int, spaceID string, token string, cid string) (int, error) {
	addedCount := 0
	for _, recipeID := range recipeIDs {
		logger.LogJSON(cid, "Tools", fmt.Sprintf("Adding recipe ID %d to book %d", recipeID, bookID), "INFO")
		_, err := uc.Tandoor.AddRecipeToBook(bookID, recipeID, spaceID, token, cid)
		if err != nil {
			logger.LogJSON(cid, "Tools", fmt.Sprintf("Failed to add recipe %d to book %d: %v", recipeID, bookID, err), "ERROR")
		} else {
			addedCount++
		}
	}
	return addedCount, nil
}
