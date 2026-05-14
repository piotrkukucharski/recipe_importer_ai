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

func (s *GeminiService) ProcessRecipe(ctx context.Context, text string, correlationID string) (*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", "Starting AI processing of extracted text", "INFO")
	model := s.Client.GenerativeModel("gemini-3-flash-preview")
	
	// Force JSON output
	model.ResponseMIMEType = "application/json"

	prompt := fmt.Sprintf(`
Przetwórz poniższy tekst i wyodrębnij z niego przepis kulinarny. 

Jeśli tekst NIE zawiera przepisu (np. jest to zwykły post, reklama, informacja o podróży), zwróć pusty obiekt JSON: {}

WAŻNE: Cały przepis, w tym jego nazwa, opis, nazwy kroków, instrukcje oraz nazwy i notatki o składnikach MUSZĄ być w języku polskim. Jeśli tekst źródłowy jest w innym języku (np. angielskim), przetłumacz go dokładnie na język polski.

Dodaj słowa kluczowe (tagi) do przepisu. Wybierz odpowiednie z poniższej listy lub dodaj własne, jeśli pasują:
wegańskie, wegetariańskie, na śniadanie, na obiad, na kolację, przekąski, na grilla, kawa, napój, herbata, smoothie, kuchnia polska, kuchnia japońska, kuchnia koreańska, kuchnia chińska, kuchnia syczuańska, drink alkoholowy, drink bezalkoholowy, naleśniki, ciasta, zupa, zupa krem, chleb, kuchania włoska, makaron, sernik, tort, sałatka.

Zwróć wynik jako ściśle sformatowany obiekt JSON (nie tablicę!) zgodny z poniższą strukturą:
{
  "name": "Nazwa przepisu (po polsku)",
  "description": "Krótki opis (po polsku)",
  "working_time": czas przygotowania w minutach (int),
  "waiting_time": czas oczekiwania w minutach (int),
  "servings": liczba porcji (int),
  "keywords": ["tag1", "tag2"],
  "steps": [
    {
      "name": "Nazwa kroku (po polsku)",
      "instruction": "Dokładna instrukcja kroku (po polsku)",
      "ingredients": [
        {
          "food": {"name": "nazwa składnika (po polsku)"},
          "unit": {"name": "jednostka (po polsku), np. g, ml, szt, łyżka"},
          "amount": ilość (float),
          "note": "dodatkowa notatka o składniku (po polsku)"
        }
      ]
    }
  ]
}

Ważne: Podziel przepis na logiczne kroki. Każdy krok musi mieć przypisane składniki, które są w nim używane. 
Jeśli w tekście nie ma jednostki, użyj pustego ciągu znaków (aplikacja wstawi domyślną). Jeśli nie ma ilości, użyj 0. Jeśli nie ma informacji o liczbie porcji, użyj 1.

Tekst do przetworzenia:
%s
`, text)

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
