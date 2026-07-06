package import_recipe

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"time"
)

type ImportTextUseCase struct {
	Processor   *Processor
	Tandoor     *tandoor.TandoorService
	TaskManager *TaskManager
}

func NewImportTextUseCase(p *Processor, t *tandoor.TandoorService, tm *TaskManager) *ImportTextUseCase {
	return &ImportTextUseCase{Processor: p, Tandoor: t, TaskManager: tm}
}

func (uc *ImportTextUseCase) Execute(ctx context.Context, text string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	uc.TaskManager.AddTask(&ImportTask{
		URL:           "Import from Text",
		CorrelationID: cid,
		Status:        "started",
		CreatedAt:     time.Now(),
		User:          username,
		Space:         spaceName,
	})

	logger.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for raw text (Multi-Recipe: %v)", multi), "INFO")

	recipes, err := uc.Processor.ProcessRecipe(ctx, text, []string{}, lang, multi, cid)
	if err != nil {
		logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage: %v", err), "ERROR")
		uc.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		logger.LogJSON(cid, "Background", "No recipes found in the provided text", "WARN")
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

func (uc *ImportTextUseCase) updateImportStatus(cid string, status string) {
	if t := uc.TaskManager.GetTask(cid); t != nil {
		t.Status = status
	}
}
