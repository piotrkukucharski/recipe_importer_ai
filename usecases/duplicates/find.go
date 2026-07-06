package duplicates

import (
	"context"
	"recipe_importer_ai/infrastructure/tandoor"
	"strings"
)

type FindUseCase struct {
	Tandoor *tandoor.TandoorService
}

func NewFindUseCase(t *tandoor.TandoorService) *FindUseCase {
	return &FindUseCase{Tandoor: t}
}

func (uc *FindUseCase) Execute(ctx context.Context, spaceID string, token string, cid string) ([]DuplicateGroup, error) {
	recipes, err := uc.Tandoor.GetRecipes(spaceID, token, cid)
	if err != nil {
		return nil, err
	}

	titleGroups := make(map[string][]map[string]interface{})
	urlGroups := make(map[string][]map[string]interface{})

	for _, recipe := range recipes {
		name, _ := recipe["name"].(string)
		nameKey := strings.ToLower(strings.TrimSpace(name))
		if nameKey != "" {
			titleGroups[nameKey] = append(titleGroups[nameKey], recipe)
		}

		if sourceURLVal, exists := recipe["source_url"]; exists && sourceURLVal != nil {
			if sourceURL, ok := sourceURLVal.(string); ok && sourceURL != "" {
				urlGroups[sourceURL] = append(urlGroups[sourceURL], recipe)
			}
		}
	}

	var duplicateGroups []DuplicateGroup

	// Add title duplicate groups (groups with size > 1)
	for key, group := range titleGroups {
		if len(group) > 1 {
			duplicateGroups = append(duplicateGroups, DuplicateGroup{
				Strategy: "title",
				Key:      key,
				Recipes:  group,
			})
		}
	}

	// Add url duplicate groups (groups with size > 1)
	for key, group := range urlGroups {
		if len(group) > 1 {
			duplicateGroups = append(duplicateGroups, DuplicateGroup{
				Strategy: "source_url",
				Key:      key,
				Recipes:  group,
			})
		}
	}

	return duplicateGroups, nil
}
