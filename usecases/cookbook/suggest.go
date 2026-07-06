package cookbook

import (
	"context"
	"recipe_importer_ai/infrastructure/tandoor"
	"recipe_importer_ai/models"
	"strings"
)

type SuggestUseCase struct {
	Tandoor      *tandoor.TandoorService
	TagsUseCase  *TagsUseCase
	MatchUseCase *MatchUseCase
}

func NewSuggestUseCase(t *tandoor.TandoorService, tu *TagsUseCase, mu *MatchUseCase) *SuggestUseCase {
	return &SuggestUseCase{Tandoor: t, TagsUseCase: tu, MatchUseCase: mu}
}

type SuggestProgressReporter func(status string)

func (uc *SuggestUseCase) Suggest(ctx context.Context, spaceID string, bookID int, token string, cid string, report SuggestProgressReporter) ([]map[string]interface{}, error) {
	// 1. Get book details
	report("Fetching recipe book details...")
	book, err := uc.Tandoor.GetRecipeBook(bookID, spaceID, token, cid)
	if err != nil {
		return nil, err
	}
	bookName, _ := book["name"].(string)
	bookDesc, _ := book["description"].(string)

	// 1b. Fetch all used tags and select related ones
	relatedTagsMap, err := uc.TagsUseCase.SelectRelatedTags(ctx, bookName, bookDesc, spaceID, token, cid)
	if err != nil {
		return nil, err
	}

	// 2. Get book entries
	report("Fetching recipes currently in the book...")
	bookEntries, err := uc.Tandoor.GetRecipeBookEntries(bookID, spaceID, token, cid)
	if err != nil {
		return nil, err
	}

	inBookMap := make(map[int]bool)
	for _, entry := range bookEntries {
		if recipeIDVal, exists := entry["recipe"]; exists {
			var recipeID int
			if rf, ok := recipeIDVal.(float64); ok {
				recipeID = int(rf)
			} else if ri, ok := recipeIDVal.(int); ok {
				recipeID = ri
			}
			if recipeID > 0 {
				inBookMap[recipeID] = true
			}
		}
	}

	// 3. Get all recipes in space
	report("Fetching all recipes in the space...")
	allRecipes, err := uc.Tandoor.GetRecipes(spaceID, token, cid)
	if err != nil {
		return nil, err
	}

	var candidates []map[string]interface{}
	var autoMatchedRecipes []map[string]interface{}

	recipeHasTag := func(r map[string]interface{}, relatedTagsMap map[string]bool) bool {
		if kws, ok := r["keywords"].([]interface{}); ok {
			for _, kw := range kws {
				if kwMap, ok := kw.(map[string]interface{}); ok {
					if label, ok := kwMap["label"].(string); ok && label != "" {
						if relatedTagsMap[strings.ToLower(label)] {
							return true
						}
					}
					if name, ok := kwMap["name"].(string); ok && name != "" {
						if relatedTagsMap[strings.ToLower(name)] {
							return true
						}
					}
				}
			}
		}
		return false
	}

	for _, r := range allRecipes {
		id := models.GetRecipeID(r)
		if id > 0 && !inBookMap[id] {
			if recipeHasTag(r, relatedTagsMap) {
				autoMatchedRecipes = append(autoMatchedRecipes, r)
			} else {
				candidates = append(candidates, r)
			}
		}
	}

	if len(candidates) == 0 && len(autoMatchedRecipes) == 0 {
		return []map[string]interface{}{}, nil
	}

	// 4. Get 10 newest examples currently in the book
	var existingExamples []map[string]interface{}
	// Sort bookEntries by id descending to get the newest
	for i := 0; i < len(bookEntries); i++ {
		for j := i + 1; j < len(bookEntries); j++ {
			idI := models.GetRecipeID(bookEntries[i])
			idJ := models.GetRecipeID(bookEntries[j])
			if idJ > idI {
				bookEntries[i], bookEntries[j] = bookEntries[j], bookEntries[i]
			}
		}
	}
	for i := 0; i < len(bookEntries) && len(existingExamples) < 10; i++ {
		entry := bookEntries[i]
		if recipeContent, ok := entry["recipe_content"].(map[string]interface{}); ok {
			existingExamples = append(existingExamples, recipeContent)
		}
	}

	// 5. Ask Gemini to classify remaining candidates (ignoring tag-matched ones)
	var matchedIDs []int
	if len(candidates) > 0 {
		report("Analyzing remaining recipes using Gemini AI...")
		if len(candidates) > 100 {
			candidates = candidates[:100]
		}
		var err error
		matchedIDs, err = uc.MatchUseCase.ClassifyRecipesForBook(ctx, bookName, bookDesc, existingExamples, candidates, cid)
		if err != nil {
			return nil, err
		}
	}

	matchedMap := make(map[int]bool)
	for _, id := range matchedIDs {
		matchedMap[id] = true
	}

	var suggestedRecipes []map[string]interface{}
	// Add all auto-matched recipes first
	suggestedRecipes = append(suggestedRecipes, autoMatchedRecipes...)

	// Add recipes classified as matching by Gemini
	for _, r := range candidates {
		id := models.GetRecipeID(r)
		if id > 0 && matchedMap[id] {
			suggestedRecipes = append(suggestedRecipes, r)
		}
	}

	return suggestedRecipes, nil
}
