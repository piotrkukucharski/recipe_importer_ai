package copy_space

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"html/template"
	"recipe_importer_ai/infrastructure/gemini"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/models"
	"strings"
)

//go:embed prompts/detect_lang.txt
var detectLangPromptTpl string

//go:embed prompts/translate_recipe.txt
var translateRecipePromptTpl string

type Translator struct {
	Gemini *gemini.GeminiService
}

func NewTranslator(g *gemini.GeminiService) *Translator {
	return &Translator{Gemini: g}
}

func (t *Translator) DetectLanguage(ctx context.Context, text string, cid string) (string, error) {
	tmpl, err := template.New("detect_lang").Parse(detectLangPromptTpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"Text": text,
	})
	if err != nil {
		return "", err
	}

	res, err := t.Gemini.GenerateRawText(ctx, "gemini-3.5-flash", buf.String())
	if err != nil {
		return "", err
	}

	return strings.ToLower(strings.TrimSpace(res)), nil
}

func (t *Translator) TranslateRecipe(ctx context.Context, recipe *models.Recipe, targetLang string, cid string) (*models.Recipe, error) {
	iso := getISO639_3(targetLang)

	recipeBytes, err := json.Marshal(recipe)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("translate_recipe").Parse(translateRecipePromptTpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]string{
		"TargetLanguage":    targetLang,
		"TargetLanguageISO": iso,
		"RecipeJSON":        string(recipeBytes),
	})
	if err != nil {
		return nil, err
	}

	logger.LogJSON(cid, "Gemini", "Translating recipe structure using LLM", "INFO")
	rawJSON, err := t.Gemini.GenerateJSON(ctx, "gemini-3.5-flash", buf.String())
	if err != nil {
		return nil, err
	}

	jsonStr := cleanJSON(rawJSON)

	var translatedRecipe models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &translatedRecipe); err != nil {
		logger.LogJSON(cid, "Gemini", "Failed to unmarshal translated recipe JSON. Raw: "+rawJSON, "ERROR")
		return nil, err
	}

	return &translatedRecipe, nil
}

func getISO639_3(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "polish", "pl", "polski":
		return "pol"
	case "english", "en", "angielski":
		return "eng"
	case "german", "de", "niemiecki":
		return "deu"
	case "spanish", "es", "hiszpański":
		return "spa"
	case "french", "fr", "francuski":
		return "fra"
	case "italian", "it", "włoski":
		return "ita"
	default:
		if len(l) >= 3 {
			return l[:3]
		}
		return "eng"
	}
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
