package import_recipe

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/models"
	"time"
)

type ImportImageUseCase struct {
	Processor   *Processor
	Tandoor     *tandoor.TandoorService
	TaskManager *TaskManager
}

func NewImportImageUseCase(p *Processor, t *tandoor.TandoorService, tm *TaskManager) *ImportImageUseCase {
	return &ImportImageUseCase{Processor: p, Tandoor: t, TaskManager: tm}
}

func (uc *ImportImageUseCase) ExecuteImages(ctx context.Context, images [][]byte, mimeTypes []string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	uc.TaskManager.AddTask(&ImportTask{
		URL:           "Import from Images",
		CorrelationID: cid,
		Status:        "started",
		CreatedAt:     time.Now(),
		User:          username,
		Space:         spaceName,
	})

	logger.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for %d images (Multi-Recipe: %v)", len(images), multi), "INFO")

	recipes, err := uc.Processor.ProcessRecipeFromImages(ctx, images, mimeTypes, lang, multi, cid)
	if err != nil {
		logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		uc.updateImportStatus(cid, "finished")
		return
	}

	uc.saveRecipesWithDishImage(ctx, recipes, images, mimeTypes, spaceID, token, cid)
}

func (uc *ImportImageUseCase) ExecuteTextAndImages(ctx context.Context, images [][]byte, mimeTypes []string, text string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	uc.TaskManager.AddTask(&ImportTask{
		URL:           "Import from Text & Images",
		CorrelationID: cid,
		Status:        "started",
		CreatedAt:     time.Now(),
		User:          username,
		Space:         spaceName,
	})

	logger.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for %d images and recipe text (Multi-Recipe: %v)", len(images), multi), "INFO")

	recipes, err := uc.Processor.ProcessRecipeFromImagesAndText(ctx, images, mimeTypes, text, lang, multi, cid)
	if err != nil {
		logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		uc.updateImportStatus(cid, "finished")
		return
	}

	uc.saveRecipesWithDishImage(ctx, recipes, images, mimeTypes, spaceID, token, cid)
}

func (uc *ImportImageUseCase) saveRecipesWithDishImage(ctx context.Context, recipes []*models.Recipe, images [][]byte, mimeTypes []string, spaceID string, token string, cid string) {
	if len(recipes) == 0 {
		logger.LogJSON(cid, "Background", "No recipes found in the uploaded sources", "WARN")
		uc.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		createdRecipe, err := uc.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for recipe %s: %v", recipe.Name, err), "ERROR")
			continue
		}

		if createdRecipe != nil {
			recipeID := int(createdRecipe["id"].(float64))
			
			if recipe.DishImageIndex != nil && *recipe.DishImageIndex >= 0 && *recipe.DishImageIndex < len(images) {
				idx := *recipe.DishImageIndex
				logger.LogJSON(cid, "Background", fmt.Sprintf("Gemini identified image at index %d as the finished dish for %s. Uploading to Tandoor...", idx, recipe.Name), "INFO")
				
				err = uc.Tandoor.UpdateImageFileMultipartWithRetry(recipeID, images[idx], mimeTypes[idx], spaceID, token, cid)
				if err != nil {
					logger.LogJSON(cid, "Background", fmt.Sprintf("Warning: failed to upload recipe image file: %v", err), "WARN")
				}
			}

			logger.BroadcastRecipe(cid, createdRecipe)
			logger.LogJSON(cid, "Background", fmt.Sprintf("Pipeline completed successfully for recipe: %s", recipe.Name), "INFO")
			importedCount++
		}
	}

	if importedCount > 0 {
		uc.updateImportStatus(cid, "imported")
	} else {
		uc.updateImportStatus(cid, "finished")
	}
}

func (uc *ImportImageUseCase) updateImportStatus(cid string, status string) {
	if t := uc.TaskManager.GetTask(cid); t != nil {
		t.Status = status
	}
}
