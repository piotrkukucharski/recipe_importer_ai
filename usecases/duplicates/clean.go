package duplicates

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/models"
)

type CleanUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewCleanUseCase(t *tandoor.TandoorService) *CleanUseCase {
	return &CleanUseCase{Tandoor: t}
}

func (uc *CleanUseCase) Execute(ctx context.Context, spaceID string, groups []DuplicateGroup, token string, cid string) (int, error) {
	deletedCount := 0
	for _, group := range groups {
		if len(group.Recipes) <= 1 {
			continue
		}

		// Sort recipes by ID descending (newest ID first)
		recipes := group.Recipes
		for i := 0; i < len(recipes); i++ {
			for j := i + 1; j < len(recipes); j++ {
				idI := models.GetRecipeID(recipes[i])
				idJ := models.GetRecipeID(recipes[j])
				if idJ > idI {
					recipes[i], recipes[j] = recipes[j], recipes[i]
				}
			}
		}

		// Keep the first one (newest), delete all the rest
		for i := 1; i < len(recipes); i++ {
			recipeID := models.GetRecipeID(recipes[i])
			if recipeID > 0 {
				recipeIDStr := fmt.Sprintf("%d", recipeID)
				logger.LogJSON(cid, "Tools", fmt.Sprintf("Cleaning duplicate recipe ID %s (%s) from group key '%s'", recipeIDStr, recipes[i]["name"], group.Key), "INFO")
				err := uc.Tandoor.DeleteRecipe(recipeIDStr, token, cid)
				if err != nil {
					logger.LogJSON(cid, "Tools", fmt.Sprintf("Failed to delete recipe duplicate %s: %v", recipeIDStr, err), "ERROR")
				} else {
					deletedCount++
				}
			}
		}
	}
	return deletedCount, nil
}
