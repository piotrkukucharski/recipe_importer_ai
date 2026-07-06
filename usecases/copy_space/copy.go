package copy_space

import (
	"context"
	"fmt"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/models"
)

type CopyUseCase struct {
	Tandoor    *tandoor.TandoorService
	Translator *Translator
}

func NewCopyUseCase(t *tandoor.TandoorService, tr *Translator) *CopyUseCase {
	return &CopyUseCase{Tandoor: t, Translator: tr}
}

type CopyProgressReporter func(status string)

func (uc *CopyUseCase) Copy(ctx context.Context, mode string, itemIDs []int, sourceSpace string, targetSpace string, targetLang string, importTags bool, token string, cid string, report CopyProgressReporter) error {
	if mode == "recipes" {
		for index, recipeID := range itemIDs {
			report(fmt.Sprintf("[%d/%d] Fetching recipe ID %d...", index+1, len(itemIDs), recipeID))
			
			rawRecipe, err := uc.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/recipe/%d/", recipeID), sourceSpace, token, cid)
			if err != nil {
				return fmt.Errorf("failed to get recipe %d: %w", recipeID, err)
			}

			recipeObj := mapTandoorToRecipe(rawRecipe, importTags)

			if err := uc.processLanguageAndTranslation(ctx, recipeObj, targetLang, cid, report); err != nil {
				return err
			}

			report(fmt.Sprintf("Saving recipe '%s' to target space...", recipeObj.Name))
			_, err = uc.Tandoor.SaveRecipe(recipeObj, targetSpace, token, cid)
			if err != nil {
				return fmt.Errorf("failed to save recipe '%s': %w", recipeObj.Name, err)
			}
		}
	} else if mode == "books" {
		for index, bookID := range itemIDs {
			report(fmt.Sprintf("[%d/%d] Fetching cookbook ID %d...", index+1, len(itemIDs), bookID))
			
			bookObj, err := uc.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/recipe-book/%d/", bookID), sourceSpace, token, cid)
			if err != nil {
				return fmt.Errorf("failed to get cookbook %d: %w", bookID, err)
			}

			bookName, _ := bookObj["name"].(string)
			bookDesc, _ := bookObj["description"].(string)

			report(fmt.Sprintf("Creating cookbook '%s' in target space...", bookName))
			createdBook, err := uc.Tandoor.PostWithRetry("/api/recipe-book/", map[string]interface{}{
				"name":        bookName,
				"description": bookDesc,
			}, targetSpace, token, cid)
			if err != nil {
				return fmt.Errorf("failed to create cookbook '%s' in target space: %w", bookName, err)
			}
			newBookID := int(createdBook["id"].(float64))

			report(fmt.Sprintf("Fetching recipes for cookbook '%s'...", bookName))
			entries, err := uc.Tandoor.GetRecipeBookEntries(bookID, sourceSpace, token, cid)
			if err != nil {
				return fmt.Errorf("failed to fetch recipes for cookbook %s: %w", bookName, err)
			}

			for idx, entry := range entries {
				recipeIDVal, _ := entry["recipe"].(float64)
				subRecipeID := int(recipeIDVal)
				if subRecipeID <= 0 {
					continue
				}

				report(fmt.Sprintf("Cookbook '%s': [%d/%d] Fetching recipe ID %d...", bookName, idx+1, len(entries), subRecipeID))
				rawRecipe, err := uc.Tandoor.GetSingleWithRetry(fmt.Sprintf("/api/recipe/%d/", subRecipeID), sourceSpace, token, cid)
				if err != nil {
					return fmt.Errorf("failed to get recipe %d: %w", subRecipeID, err)
				}

				recipeObj := mapTandoorToRecipe(rawRecipe, importTags)

				if err := uc.processLanguageAndTranslation(ctx, recipeObj, targetLang, cid, report); err != nil {
					return err
				}

				report(fmt.Sprintf("Saving recipe '%s' to target space...", recipeObj.Name))
				createdRecipe, err := uc.Tandoor.SaveRecipe(recipeObj, targetSpace, token, cid)
				if err != nil {
					return fmt.Errorf("failed to save recipe '%s': %w", recipeObj.Name, err)
				}

				if createdRecipe != nil {
					newRecipeID := int(createdRecipe["id"].(float64))
					report(fmt.Sprintf("Adding recipe '%s' to target cookbook...", recipeObj.Name))
					_, err = uc.Tandoor.AddRecipeToBook(newBookID, newRecipeID, targetSpace, token, cid)
					if err != nil {
						logger.LogJSON(cid, "Tools", fmt.Sprintf("Warning: failed to add recipe %d to cookbook %d: %v", newRecipeID, newBookID, err), "WARN")
					}
				}
			}
		}
	}

	return nil
}

func (uc *CopyUseCase) processLanguageAndTranslation(ctx context.Context, recipeObj *models.Recipe, targetLang string, cid string, report CopyProgressReporter) error {
	report(fmt.Sprintf("Detecting language for recipe '%s'...", recipeObj.Name))
	
	textToDetect := recipeObj.Name + "\n" + recipeObj.Description
	if len(recipeObj.Steps) > 0 {
		textToDetect += "\n" + recipeObj.Steps[0].Instruction
	}
	
	detectedISO, err := uc.Translator.DetectLanguage(ctx, textToDetect, cid)
	if err != nil {
		logger.LogJSON(cid, "Tools", fmt.Sprintf("Language detection failed: %v", err), "WARN")
		detectedISO = "unknown"
	}
	
	targetISO := getISO639_3(targetLang)
	logger.LogJSON(cid, "Tools", fmt.Sprintf("Recipe language: %s, target language: %s (ISO: %s)", detectedISO, targetLang, targetISO), "INFO")

	if detectedISO != targetISO && detectedISO != "unknown" {
		report(fmt.Sprintf("Translating recipe '%s' from %s to %s...", recipeObj.Name, detectedISO, targetLang))
		translated, err := uc.Translator.TranslateRecipe(ctx, recipeObj, targetLang, cid)
		if err != nil {
			logger.LogJSON(cid, "Tools", fmt.Sprintf("Translation failed, falling back to original: %v", err), "WARN")
		} else {
			translated.SourceURL = recipeObj.SourceURL
			translated.ImageURL = recipeObj.ImageURL
			*recipeObj = *translated
		}
	}

	return nil
}

func mapTandoorToRecipe(tandoorRecipe map[string]interface{}, importTags bool) *models.Recipe {
	r := &models.Recipe{}
	if val, ok := tandoorRecipe["name"].(string); ok {
		r.Name = val
	}
	if val, ok := tandoorRecipe["description"].(string); ok {
		r.Description = val
	}
	if val, ok := tandoorRecipe["working_time"].(float64); ok {
		r.WorkingTime = int(val)
	}
	if val, ok := tandoorRecipe["waiting_time"].(float64); ok {
		r.WaitingTime = int(val)
	}
	if val, ok := tandoorRecipe["servings"].(float64); ok {
		r.Servings = int(val)
	}
	if val, ok := tandoorRecipe["source_url"].(string); ok {
		r.SourceURL = val
	}
	if val, ok := tandoorRecipe["image_url"].(string); ok {
		r.ImageURL = val
	}

	if importTags {
		if kws, ok := tandoorRecipe["keywords"].([]interface{}); ok {
			for _, kw := range kws {
				if kwMap, ok := kw.(map[string]interface{}); ok {
					if name, ok := kwMap["name"].(string); ok && name != "" {
						r.Keywords = append(r.Keywords, name)
					}
				}
			}
		}
	}

	if steps, ok := tandoorRecipe["steps"].([]interface{}); ok {
		for _, step := range steps {
			if stepMap, ok := step.(map[string]interface{}); ok {
				s := models.Step{}
				if name, ok := stepMap["name"].(string); ok {
					s.Name = name
				}
				if instruction, ok := stepMap["instruction"].(string); ok {
					s.Instruction = instruction
				}

				if ingredients, ok := stepMap["ingredients"].([]interface{}); ok {
					for _, ing := range ingredients {
						if ingMap, ok := ing.(map[string]interface{}); ok {
							i := models.Ingredient{}
							if amount, ok := ingMap["amount"].(float64); ok {
								i.Amount = amount
							}
							if note, ok := ingMap["note"].(string); ok {
								i.Note = note
							}

							if food, ok := ingMap["food"].(map[string]interface{}); ok {
								if fName, ok := food["name"].(string); ok {
									i.Food.Name = fName
								}
							}

							if unit, ok := ingMap["unit"].(map[string]interface{}); ok {
								if uName, ok := unit["name"].(string); ok {
									i.Unit.Name = uName
								}
							}
							s.Ingredients = append(s.Ingredients, i)
						}
					}
				}
				r.Steps = append(r.Steps, s)
			}
		}
	}

	return r
}
