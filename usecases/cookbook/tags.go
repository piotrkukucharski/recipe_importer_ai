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
	"recipe_importer_ai/infrastructure/tandoor"
	"strings"
)

//go:embed prompts/select_tags.txt
var selectTagsPromptTpl string

type TagsUseCase struct {
	Tandoor *tandoor.TandoorService
	Gemini  *gemini.GeminiService
}

func NewTagsUseCase(t *tandoor.TandoorService, g *gemini.GeminiService) *TagsUseCase {
	return &TagsUseCase{Tandoor: t, Gemini: g}
}

func (uc *TagsUseCase) SelectRelatedTags(ctx context.Context, bookName string, bookDesc string, spaceID string, token string, cid string) (map[string]bool, error) {
	logger.LogJSON(cid, "Gemini", "Fetching all recipe tags/keywords...", "INFO")
	keywords, err := uc.Tandoor.GetKeywords(spaceID, token, cid)
	if err != nil {
		return nil, err
	}

	var tagNames []string
	for _, kw := range keywords {
		if name, ok := kw["name"].(string); ok && name != "" {
			tagNames = append(tagNames, name)
		}
	}

	if len(tagNames) == 0 {
		return make(map[string]bool), nil
	}

	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Filtering %d tags for book '%s' using AI", len(tagNames), bookName), "INFO")

	// Parse the template
	tmpl, err := template.New("select_tags").Parse(selectTagsPromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"BookName":        bookName,
		"BookDescription": bookDesc,
		"Tags":            strings.Join(tagNames, "\n"),
	})
	if err != nil {
		return nil, err
	}

	// Call Gemini
	rawJSON, err := uc.Gemini.GenerateJSON(ctx, "gemini-2.5-flash", buf.String())
	if err != nil {
		return nil, err
	}

	// Extract JSON
	jsonStr := extractJSON(rawJSON)

	var result struct {
		SelectedTags []string `json:"selected_tags"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.LogJSON(cid, "Gemini", fmt.Sprintf("Failed to unmarshal SelectedTags: %s. Raw: %s", err, rawJSON), "ERROR")
		return nil, err
	}

	relatedTagsMap := make(map[string]bool)
	for _, tag := range result.SelectedTags {
		relatedTagsMap[strings.ToLower(tag)] = true
	}

	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Tag filtering complete. Selected tags: %v", result.SelectedTags), "INFO")
	return relatedTagsMap, nil
}

// Helper to extract JSON block from raw response
func extractJSON(s string) string {
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
