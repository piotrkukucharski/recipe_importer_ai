package cookbook

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/models"
	"strings"
)

//go:embed prompts/classify.txt
var classifyPromptTpl string

type MatchUseCase struct {
	Gemini *gemini.GeminiService
}

func NewMatchUseCase(g *gemini.GeminiService) *MatchUseCase {
	return &MatchUseCase{Gemini: g}
}

func (uc *MatchUseCase) ClassifyRecipesForBook(ctx context.Context, bookName string, bookDesc string, existingRecipes []map[string]interface{}, candidates []map[string]interface{}, correlationID string) ([]int, error) {
	logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("Classifying %d recipes for book '%s' using AI", len(candidates), bookName), "INFO")
	
	if len(candidates) == 0 {
		return []int{}, nil
	}

	// Format existing recipes as examples
	var examplesBuilder strings.Builder
	for _, r := range existingRecipes {
		name, _ := r["name"].(string)
		desc, _ := r["description"].(string)
		keywordsList := []string{}
		if kws, ok := r["keywords"].([]interface{}); ok {
			for _, kw := range kws {
				if kwMap, ok := kw.(map[string]interface{}); ok {
					if label, ok := kwMap["label"].(string); ok {
						keywordsList = append(keywordsList, label)
					}
				}
			}
		}
		examplesBuilder.WriteString(fmt.Sprintf("- Name: %s, Description: %s, Tags: %v\n", name, desc, keywordsList))
	}

	// Format candidates
	var candidatesBuilder strings.Builder
	for _, r := range candidates {
		id := models.GetRecipeID(r)
		name, _ := r["name"].(string)
		desc, _ := r["description"].(string)
		keywordsList := []string{}
		if kws, ok := r["keywords"].([]interface{}); ok {
			for _, kw := range kws {
				if kwMap, ok := kw.(map[string]interface{}); ok {
					if label, ok := kwMap["label"].(string); ok {
						keywordsList = append(keywordsList, label)
					}
				}
			}
		}
		candidatesBuilder.WriteString(fmt.Sprintf("- ID: %d, Name: %s, Description: %s, Tags: %v\n", id, name, desc, keywordsList))
	}

	tmpl, err := template.New("classify").Parse(classifyPromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"BookName":        bookName,
		"BookDescription": bookDesc,
		"Examples":        examplesBuilder.String(),
		"Candidates":      candidatesBuilder.String(),
	})
	if err != nil {
		return nil, err
	}

	rawJSON, err := uc.Gemini.GenerateJSON(ctx, "gemini-2.5-flash", buf.String())
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(rawJSON)

	var result struct {
		MatchedRecipeIDs []int `json:"matched_recipe_ids"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("Failed to unmarshal Gemini classification: %s. Raw: %s", err, rawJSON), "ERROR")
		return nil, err
	}

	logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("AI classification complete. Found %d matching recipes", len(result.MatchedRecipeIDs)), "INFO")
	return result.MatchedRecipeIDs, nil
}
