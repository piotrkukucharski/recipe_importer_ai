package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"recipe_importer_ai/models"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type GeminiService struct {
	Client *genai.Client
}

func NewGeminiService(ctx context.Context) (*GeminiService, error) {
	apiKey := os.Getenv("GEMINI_KEY")
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &GeminiService{Client: client}, nil
}

func (s *GeminiService) ProcessRecipe(ctx context.Context, text string, targetLanguage string, correlationID string) (*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Starting AI processing of extracted text (Target Language: %s)", targetLanguage), "INFO")
	model := s.Client.GenerativeModel("gemini-3-flash-preview")
	
	// Force JSON output
	model.ResponseMIMEType = "application/json"

	if targetLanguage == "" {
		targetLanguage = "Polish"
	}

	prompt := fmt.Sprintf(`
Process the following text and extract a culinary recipe from it.

If the text does NOT contain a recipe (e.g., it is a regular post, advertisement, travel info), return an empty JSON object: {}

IMPORTANT: The entire recipe, including its name, description, step names, instructions, and ingredient names/notes MUST be in the following language: %s. If the source text is in a different language, translate it accurately to %s.

Add keywords (tags) to the recipe. Choose appropriate ones from the list below or add your own if they fit (translate them to %s as well):
vegan, vegetarian, for breakfast, for dinner, for lunch, snacks, for grill, coffee, drink, tea, smoothie, Polish cuisine, Japanese cuisine, Korean cuisine, Chinese cuisine, Sichuan cuisine, alcoholic drink, non-alcoholic drink, pancakes, cakes, soup, cream soup, bread, Italian cuisine, pasta, cheesecake, cake, salad.

Return the result as a strictly formatted JSON object (not an array!) matching the structure below:
{
  "name": "Recipe Name (in %s)",
  "description": "Short description (in %s)",
  "working_time": preparation time in minutes (int),
  "waiting_time": waiting time in minutes (int),
  "servings": number of servings (int),
  "keywords": ["tag1", "tag2"],
  "steps": [
    {
      "name": "Step Name (in %s)",
      "instruction": "Detailed step instruction (in %s)",
      "ingredients": [
        {
          "food": {"name": "ingredient name (in %s)"},
          "unit": {"name": "unit (in %s), e.g., g, ml, pcs, tbsp"},
          "amount": amount (float),
          "note": "additional ingredient note (in %s)"
        }
      ]
    }
  ]
}

Important: Divide the recipe into logical steps. Each step must have the ingredients assigned that are used in it.
If there is no unit in the text, use an empty string. If there is no amount, use 0. If there is no servings info, use 1.

Text to process:
%s
`, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, text)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Error generating content: %v", err), "ERROR")
		return nil, err
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		LogJSON(correlationID, "Gemini", "No candidates or content in response", "ERROR")
		return nil, fmt.Errorf("no response from gemini")
	}

	var fullResponse strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		fullResponse.WriteString(fmt.Sprintf("%v", part))
	}

	jsonStr := fullResponse.String()
	jsonStr = strings.TrimPrefix(jsonStr, "```json")
	jsonStr = strings.TrimSuffix(jsonStr, "```")
	jsonStr = strings.TrimSpace(jsonStr)

	// Check if it's an empty object (not a recipe)
	if jsonStr == "{}" || jsonStr == "" {
		LogJSON(correlationID, "Gemini", "Text does not contain a recipe, skipping", "INFO")
		return nil, nil
	}

	var recipe models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &recipe); err != nil {
		// Fallback for array response
		var recipes []models.Recipe
		if arrErr := json.Unmarshal([]byte(jsonStr), &recipes); arrErr == nil && len(recipes) > 0 {
			LogJSON(correlationID, "Gemini", "Successfully unmarshaled from array format", "INFO")
			return &recipes[0], nil
		}
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Failed to unmarshal JSON: %v. Raw: %s", err, jsonStr), "ERROR")
		return nil, fmt.Errorf("failed to unmarshal gemini response: %w, response: %s", err, jsonStr)
	}

    if recipe.Name == "" {
        LogJSON(correlationID, "Gemini", "Recipe name is empty, likely not a recipe", "INFO")
        return nil, nil
    }

	LogJSON(correlationID, "Gemini", fmt.Sprintf("Successfully unmarshaled recipe: %s", recipe.Name), "INFO")
	return &recipe, nil
}
