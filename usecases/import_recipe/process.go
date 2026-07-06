package import_recipe

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"io"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/models"
	"strconv"
	"strings"
	"time"
)

//go:embed prompts/extract_recipe.txt
var extractRecipePromptTpl string

//go:embed prompts/evaluate_image.txt
var evaluateImagePromptTpl string

type Processor struct {
	Gemini *gemini.GeminiService
}

func NewProcessor(g *gemini.GeminiService) *Processor {
	return &Processor{Gemini: g}
}

func (p *Processor) ProcessRecipe(ctx context.Context, text string, imageURLs []string, targetLanguage string, multi bool, cid string) ([]*models.Recipe, error) {
	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Starting AI processing of extracted text (Target Language: %s, Multi-Recipe: %v)", targetLanguage, multi), "INFO")

	recipeQuantityInstruction := "Process the following text and extract exactly ONE culinary recipe from it. Even if the source text could contain multiple recipes, assume there is only one main recipe and ignore any others."
	if multi {
		recipeQuantityInstruction = "Process the following text and extract all culinary recipes from it. A single source text can contain multiple recipes. Return all extracted recipes."
	}

	tmpl, err := template.New("extract_recipe").Parse(extractRecipePromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"QuantityInstruction": recipeQuantityInstruction,
		"TargetLanguage":      targetLanguage,
		"ImagesList":          strings.Join(imageURLs, "\n"),
		"Text":                text,
	})
	if err != nil {
		return nil, err
	}

	rawJSON, err := p.Gemini.GenerateJSON(ctx, "gemini-3.1-pro-preview", buf.String())
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(rawJSON)
	return parseRecipesJSON(jsonStr, rawJSON, cid)
}

func (p *Processor) ProcessRecipeFromImages(ctx context.Context, images [][]byte, mimeTypes []string, targetLanguage string, multi bool, cid string) ([]*models.Recipe, error) {
	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Starting AI processing of %d images (Target Language: %s, Multi-Recipe: %v)", len(images), targetLanguage, multi), "INFO")

	recipeQuantityInstruction := "Analyze the provided images of culinary recipes. Extract exactly ONE culinary recipe from these images. Even if they contain multiple recipes, assume there is only one main recipe and ignore any others."
	if multi {
		recipeQuantityInstruction = "Analyze the provided images of culinary recipes. They may be photos of a cookbook, screenshots, or food photos. A single source can contain multiple recipes. Extract all recipes from these images."
	}

	tmpl, err := template.New("extract_recipe").Parse(extractRecipePromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"QuantityInstruction": recipeQuantityInstruction,
		"TargetLanguage":      targetLanguage,
		"ImagesList":          "",
		"Text":                "",
	})
	if err != nil {
		return nil, err
	}

	rawJSON, err := p.Gemini.GenerateFromImages(ctx, "gemini-3.1-pro-preview", buf.String(), images, mimeTypes)
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(rawJSON)
	return parseRecipesJSON(jsonStr, rawJSON, cid)
}

func (p *Processor) ProcessRecipeFromImagesAndText(ctx context.Context, images [][]byte, mimeTypes []string, text string, targetLanguage string, multi bool, cid string) ([]*models.Recipe, error) {
	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Starting AI processing of %d images and text (Target Language: %s, Multi-Recipe: %v)", len(images), targetLanguage, multi), "INFO")

	recipeQuantityInstruction := "Analyze the provided images and the accompanying text of culinary recipes. Extract exactly ONE culinary recipe from these images and text. Even if they contain multiple recipes, assume there is only one main recipe and ignore any others."
	if multi {
		recipeQuantityInstruction = "Analyze the provided images and the accompanying text of culinary recipes. They may be photos of a cookbook, screenshots, food photos, and/or pasted text containing ingredients, steps, description. A single source can contain multiple recipes. Extract all recipes from these images and the text combined."
	}

	tmpl, err := template.New("extract_recipe").Parse(extractRecipePromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"QuantityInstruction": recipeQuantityInstruction,
		"TargetLanguage":      targetLanguage,
		"ImagesList":          "",
		"Text":                text,
	})
	if err != nil {
		return nil, err
	}

	rawJSON, err := p.Gemini.GenerateFromImages(ctx, "gemini-3.1-pro-preview", buf.String(), images, mimeTypes)
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(rawJSON)
	return parseRecipesJSON(jsonStr, rawJSON, cid)
}

func (p *Processor) EvaluateImage(ctx context.Context, imageURL string, recipeName string, cid string) (int, error) {
	logger.LogJSON(cid, "Gemini", fmt.Sprintf("Evaluating image visually: %s", imageURL), "INFO")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(imageURL)
	if err != nil {
		return 0, fmt.Errorf("failed to download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to download image, status: %d", resp.StatusCode)
	}

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read image data: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		if strings.Contains(imageURL, ".png") {
			contentType = "image/png"
		} else if strings.Contains(imageURL, ".webp") {
			contentType = "image/webp"
		} else {
			contentType = "image/jpeg"
		}
	}

	tmpl, err := template.New("evaluate_image").Parse(evaluateImagePromptTpl)
	if err != nil {
		return 0, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"RecipeName": recipeName,
	})
	if err != nil {
		return 0, err
	}

	rawScore, err := p.Gemini.GenerateFromImages(ctx, "gemini-3-flash-preview", buf.String(), [][]byte{imgData}, []string{contentType})
	if err != nil {
		return 0, err
	}

	score, err := strconv.Atoi(strings.TrimSpace(rawScore))
	if err != nil {
		return 0, nil
	}

	return score, nil
}

type GeminiResponseWrapper struct {
	Recipes []*models.Recipe `json:"recipes"`
}

func parseRecipesJSON(jsonStr string, rawStr string, correlationID string) ([]*models.Recipe, error) {
	if jsonStr == "{}" || jsonStr == "" {
		logger.LogJSON(correlationID, "Gemini", "JSON is empty, skipping", "INFO")
		return nil, nil
	}

	var wrapper GeminiResponseWrapper
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err == nil && len(wrapper.Recipes) > 0 {
		logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("Successfully unmarshaled %d recipes from wrapper format", len(wrapper.Recipes)), "INFO")
		var validRecipes []*models.Recipe
		for _, r := range wrapper.Recipes {
			if r != nil && r.Name != "" {
				validRecipes = append(validRecipes, r)
			}
		}
		return validRecipes, nil
	}

	var recipesSlice []*models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &recipesSlice); err == nil && len(recipesSlice) > 0 {
		logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("Successfully unmarshaled %d recipes from array format", len(recipesSlice)), "INFO")
		var validRecipes []*models.Recipe
		for _, r := range recipesSlice {
			if r != nil && r.Name != "" {
				validRecipes = append(validRecipes, r)
			}
		}
		return validRecipes, nil
	}

	var singleRecipe models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &singleRecipe); err == nil {
		if singleRecipe.Name != "" {
			logger.LogJSON(correlationID, "Gemini", "Successfully unmarshaled 1 recipe from single format", "INFO")
			return []*models.Recipe{&singleRecipe}, nil
		}
	}

	logger.LogJSON(correlationID, "Gemini", fmt.Sprintf("Failed to unmarshal JSON into any known recipe format. Raw: %s", rawStr), "ERROR")
	return nil, fmt.Errorf("failed to unmarshal gemini response")
}

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
