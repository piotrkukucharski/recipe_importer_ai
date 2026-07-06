package import_recipe

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/apify"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"time"
)

type ImportURLUseCase struct {
	Apify       *apify.ApifyService
	Processor   *Processor
	Tandoor     *tandoor.TandoorService
	TaskManager *TaskManager
}

func NewImportURLUseCase(a *apify.ApifyService, p *Processor, t *tandoor.TandoorService, tm *TaskManager) *ImportURLUseCase {
	return &ImportURLUseCase{Apify: a, Processor: p, Tandoor: t, TaskManager: tm}
}

func (uc *ImportURLUseCase) Execute(ctx context.Context, url string, spaceID string, spaceName string, username string, lang string, multi bool, token string, cid string) {
	uc.TaskManager.AddTask(&ImportTask{
		URL:           url,
		CorrelationID: cid,
		Status:        "started",
		CreatedAt:     time.Now(),
		User:          username,
		Space:         spaceName,
	})

	logger.LogJSON(cid, "Background", fmt.Sprintf("Starting processing for URL: %s (Multi-Recipe: %v)", url, multi), "INFO")

	items, err := uc.Apify.ScrapeItems(url, cid)
	if err != nil {
		logger.LogJSON(cid, "Background", fmt.Sprintf("Final failure at Scrape stage for %s: %v", url, err), "ERROR")
		uc.updateImportStatus(cid, "finished")
		return
	}

	if len(items) > 1 {
		if multi {
			logger.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d), processing as profile/batch sequentially", len(items)), "INFO")
			for _, item := range items {
				uc.processScrapedItem(ctx, item, spaceID, lang, multi, token, cid)
			}
		} else {
			logger.LogJSON(cid, "Background", fmt.Sprintf("Detected multiple items (%d) but multi-recipe mode is disabled. Processing only the first item.", len(items)), "INFO")
			uc.processScrapedItem(ctx, items[0], spaceID, lang, multi, token, cid)
		}
	} else if len(items) == 1 {
		uc.processScrapedItem(ctx, items[0], spaceID, lang, multi, token, cid)
	} else {
		logger.LogJSON(cid, "Background", "No items found to process", "WARN")
		uc.updateImportStatus(cid, "finished")
	}
}

func (uc *ImportURLUseCase) processScrapedItem(ctx context.Context, item apify.ScrapedItem, spaceID string, lang string, multi bool, token string, cid string) {
	recipes, err := uc.Processor.ProcessRecipe(ctx, item.Text, item.Images, lang, multi, cid)
	if err != nil {
		logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Gemini stage for %s: %v", item.URL, err), "ERROR")
		uc.updateImportStatus(cid, "finished")
		return
	}

	if len(recipes) == 0 {
		uc.updateImportStatus(cid, "finished")
		return
	}

	importedCount := 0
	for _, recipe := range recipes {
		recipe.SourceURL = item.URL

		bestImage := ""
		maxScore := -1
		
		candidates := item.Images
		if len(candidates) > 5 {
			candidates = candidates[:5]
		}

		logger.LogJSON(cid, "Background", fmt.Sprintf("Starting visual evaluation for %d image candidates for %s", len(candidates), recipe.Name), "INFO")
		for _, imgURL := range candidates {
			score, err := uc.Processor.EvaluateImage(ctx, imgURL, recipe.Name, cid)
			if err != nil {
				logger.LogJSON(cid, "Background", fmt.Sprintf("Failed to evaluate image %s: %v", imgURL, err), "WARN")
				continue
			}
			logger.LogJSON(cid, "Background", fmt.Sprintf("Image score %d for: %s", score, imgURL), "INFO")
			if score > maxScore {
				maxScore = score
				bestImage = imgURL
			}
			if score == 10 { break }
		}

		if bestImage != "" && maxScore >= 4 {
			recipe.ImageURL = bestImage
			logger.LogJSON(cid, "Background", fmt.Sprintf("Selected best image with score %d: %s", maxScore, bestImage), "INFO")
		} else if recipe.ImageURL == "" && item.ImageURL != "" {
			recipe.ImageURL = item.ImageURL
		}

		createdRecipe, err := uc.Tandoor.SaveRecipe(recipe, spaceID, token, cid)
		if err != nil {
			logger.LogJSON(cid, "Background", fmt.Sprintf("Failure at Tandoor stage for %s (%s): %v", item.URL, recipe.Name, err), "ERROR")
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

func (uc *ImportURLUseCase) updateImportStatus(cid string, status string) {
	if t := uc.TaskManager.GetTask(cid); t != nil {
		t.Status = status
	}
}
