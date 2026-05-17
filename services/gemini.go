package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"recipe_importer_ai/models"
	"strconv"
	"strings"
	"time"

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

func (s *GeminiService) EvaluateImage(ctx context.Context, imageURL string, recipeName string, correlationID string) (int, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Evaluating image visually: %s", imageURL), "INFO")

	// 1. Download image
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

	// Determine MIME type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || !strings.HasPrefix(contentType, "image/") {
		// Fallback or skip if not an image
		if strings.Contains(imageURL, ".png") {
			contentType = "image/png"
		} else if strings.Contains(imageURL, ".webp") {
			contentType = "image/webp"
		} else {
			contentType = "image/jpeg"
		}
	}

	model := s.Client.GenerativeModel("gemini-3-flash-preview")

	prompt := []genai.Part{
		genai.ImageData(strings.TrimPrefix(contentType, "image/"), imgData),
		genai.Text(fmt.Sprintf(`
Analyze this image in the context of a recipe for: "%s".
Rate this image on a scale of 0 to 10 based on these criteria:
- 10: Perfect, high-quality photo of the FINAL, finished dish.
- 7-9: Good photo of the finished dish, but maybe lower quality or slightly off-center.
- 4-6: Photo of the dish but looks like a step-by-step process photo or contains unnecessary elements.
- 1-3: Photo of an ingredient, or very loosely related to the dish.
- 0: Icon, logo, advertisement, or completely unrelated image.

Return ONLY the numeric score (e.g. "8"). Do not add any text.
`, recipeName)),
	}

	res, err := model.GenerateContent(ctx, prompt...)
	if err != nil {
		return 0, err
	}

	if len(res.Candidates) == 0 || res.Candidates[0].Content == nil {
		return 0, fmt.Errorf("no response from gemini for image evaluation")
	}

	var scoreStr strings.Builder
	for _, part := range res.Candidates[0].Content.Parts {
		scoreStr.WriteString(fmt.Sprintf("%v", part))
	}

	score, err := strconv.Atoi(strings.TrimSpace(scoreStr.String()))
	if err != nil {
		return 0, nil // Return 0 if we can't parse the score
	}

	return score, nil
}

func (s *GeminiService) ProcessRecipe(ctx context.Context, text string, imageURLs []string, targetLanguage string, correlationID string) (*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Starting AI processing of extracted text (Target Language: %s)", targetLanguage), "INFO")
	model := s.Client.GenerativeModel("gemini-3-flash-preview")

	// Force JSON output
	model.ResponseMIMEType = "application/json"

	if targetLanguage == "" {
		targetLanguage = "Polish"
	}

	imagesList := strings.Join(imageURLs, "\n")

	prompt := fmt.Sprintf(`
Process the following text and extract a culinary recipe from it.

If the text does NOT contain a recipe (e.g., it is a regular post, advertisement, travel info), return an empty JSON object: {}

IMPORTANT: The entire recipe, including its name, description, step names, instructions, and ingredient names/notes MUST be in the following language: %s. If the source text is in a different language, translate it accurately to %s.

IMAGE SELECTION:
Below is a list of image URLs found on the page. Analyze their filenames and paths.
Pick ONE URL that most likely represents the FINAL dish (the finished meal).
Prioritize images that:
1. Look like a main photo of the dish.
2. Have higher resolution (judging by filename/path if possible).
3. Are NOT icons, logos, avatars, or step-by-step photos.
If you find a suitable image, put its URL in the "image_url" field. If none fit well, leave it empty.

Add keywords (tags) to the recipe. Choose appropriate ones from the list below or add your own if they fit (translate them to %s as well):
vegan, vegetarian, for breakfast, for dinner, for lunch, snacks, for grill, coffee, drink, tea, smoothie, Polish cuisine, Japanese cuisine, Korean cuisine, Chinese cuisine, Sichuan cuisine, alcoholic drink, non-alcoholic drink, pancakes, cakes, soup, cream soup, bread, Italian cuisine, pasta, cheesecake, cake, salad.

Return the result as a strictly formatted JSON object (not an array!) matching the structure below:
{
  "name": "Recipe Name (in %s)",
  "description": "Short description (in %s)",
  "image_url": "the selected best image URL from the list provided below",
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

Potential Image URLs:
%s

Text to process:
%s
`, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, imagesList, text)

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
