package cleanup

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/infrastructure/tandoor"
	"strings"
)

//go:embed prompts/group_items.txt
var groupItemsPromptTpl string

type CleanupUseCase struct {
	Tandoor *tandoor.TandoorService
	Gemini  *gemini.GeminiService
}

func NewCleanupUseCase(t *tandoor.TandoorService, g *gemini.GeminiService) *CleanupUseCase {
	return &CleanupUseCase{Tandoor: t, Gemini: g}
}

type CleanupItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type MergeGroup struct {
	TargetID   int    `json:"target_id"`
	MergeIDs   []int  `json:"merge_ids"`
	TargetName string `json:"target_name"`
}

type CleanupProgressReporter func(status string)

func (uc *CleanupUseCase) Cleanup(ctx context.Context, itemType string, targetLang string, spaceID string, token string, cid string, report CleanupProgressReporter) error {
	var path string
	switch itemType {
	case "tags":
		path = "/api/keyword/"
	case "ingredients":
		path = "/api/food/"
	case "units":
		path = "/api/unit/"
	default:
		return fmt.Errorf("invalid item type: %s", itemType)
	}

	report(fmt.Sprintf("Fetching all %s from space...", itemType))
	rawItems, err := uc.Tandoor.GetAllItems(path+"?page_size=250", spaceID, token, cid)
	if err != nil {
		return fmt.Errorf("failed to fetch items: %w", err)
	}

	var items []CleanupItem
	for _, raw := range rawItems {
		idVal, _ := raw["id"].(float64)
		nameVal, _ := raw["name"].(string)
		if idVal > 0 && nameVal != "" {
			items = append(items, CleanupItem{
				ID:   int(idVal),
				Name: nameVal,
			})
		}
	}

	if len(items) == 0 {
		report("No items found to clean up.")
		return nil
	}

	report(fmt.Sprintf("Analyzing %d items using AI for duplicates and translation to %s...", len(items), targetLang))
	
	tmpl, err := template.New("group").Parse(groupItemsPromptTpl)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]interface{}{
		"TargetLanguage": targetLang,
		"Items":          items,
	})
	if err != nil {
		return err
	}

	rawJSON, err := uc.Gemini.GenerateJSON(ctx, "gemini-3.5-flash", buf.String())
	if err != nil {
		return fmt.Errorf("failed to analyze with Gemini: %w", err)
	}

	jsonStr := cleanJSON(rawJSON)
	var groups []MergeGroup
	if err := json.Unmarshal([]byte(jsonStr), &groups); err != nil {
		logger.LogJSON(cid, "Cleanup", "Failed to parse Gemini merge response: "+rawJSON, "ERROR")
		return fmt.Errorf("failed to parse AI response: %w", err)
	}

	if len(groups) == 0 {
		report("No duplicates or translation adjustments identified by AI.")
		return nil
	}

	report(fmt.Sprintf("AI identified %d merge groups. Executing merge and rename operations...", len(groups)))

	var typePath string
	switch itemType {
	case "tags":
		typePath = "/api/keyword"
	case "ingredients":
		typePath = "/api/food"
	case "units":
		typePath = "/api/unit"
	}

	for idx, group := range groups {
		report(fmt.Sprintf("[%d/%d] Renaming target item %d to '%s'...", idx+1, len(groups), group.TargetID, group.TargetName))
		_, err := uc.Tandoor.PatchWithRetry(fmt.Sprintf("%s/%d/", typePath, group.TargetID), map[string]string{
			"name": group.TargetName,
		}, spaceID, token, cid)
		if err != nil {
			logger.LogJSON(cid, "Cleanup", fmt.Sprintf("Rename warning on %d: %v", group.TargetID, err), "WARN")
		}

		for _, mergeID := range group.MergeIDs {
			report(fmt.Sprintf("[%d/%d] Merging item %d into target %d...", idx+1, len(groups), mergeID, group.TargetID))
			_, err := uc.Tandoor.PutWithRetry(fmt.Sprintf("%s/%d/merge/%d/", typePath, mergeID, group.TargetID), nil, spaceID, token, cid)
			if err != nil {
				logger.LogJSON(cid, "Cleanup", fmt.Sprintf("Merge warning: failed to merge %d into %d: %v", mergeID, group.TargetID, err), "WARN")
			}
		}
	}

	report("Clean up completed successfully!")
	return nil
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexAny(s, "{[")
	if start == -1 {
		return s
	}
	var end int
	if s[start] == '{' {
		end = strings.LastIndex(s, "}")
	} else {
		end = strings.LastIndex(s, "]")
	}
	if end == -1 || end < start {
		return s
	}
	return s[start : end+1]
}
