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

func (s *GeminiService) ProcessRecipe(ctx context.Context, text string, imageURLs []string, targetLanguage string, correlationID string) ([]*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Starting AI processing of extracted text (Target Language: %s)", targetLanguage), "INFO")
	model := s.Client.GenerativeModel("gemini-3.1-pro-preview")

	// Force JSON output
	model.ResponseMIMEType = "application/json"

	if targetLanguage == "" {
		targetLanguage = "Polish"
	}

	imagesList := strings.Join(imageURLs, "\n")

	prompt := fmt.Sprintf(`
Process the following text and extract culinary recipes from it. A single source text can contain multiple recipes.
Return all extracted recipes.

If the text does NOT contain any recipes (e.g., it is a regular post, advertisement, travel info), return an empty JSON object: {}

IMPORTANT: All recipes, including their names, descriptions, step names, instructions, and ingredient names/notes MUST be in the following language: %s. If the source text is in a different language, translate it accurately to %s.

IMAGE SELECTION:
Below is a list of image URLs found on the page. Analyze their filenames and paths.
For each recipe, pick ONE URL that most likely represents the FINAL dish (the finished meal) for that specific recipe.
Prioritize images that:
1. Look like a main photo of the dish.
2. Have higher resolution (judging by filename/path if possible).
3. Are NOT icons, logos, avatars, or step-by-step photos.
If you find a suitable image for a recipe, put its URL in the "image_url" field. If none fit well, leave it empty.

Add keywords (tags) to each recipe. Choose appropriate ones from the list below or add your own if they fit (translate them to %s as well):
vegan, vegetarian, for breakfast, for dinner, for lunch, snacks, for grill, coffee, drink, tea, smoothie, Polish cuisine, Japanese cuisine, Korean cuisine, Chinese cuisine, Sichuan cuisine, alcoholic drink, non-alcoholic drink, pancakes, cakes, soup, cream soup, bread, Italian cuisine, pasta, cheesecake, cake, salad.

Return the result as a strictly formatted JSON object matching the structure below:
{
  "recipes": [
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
  ]
}

INGREDIENT EXTRACTION RULES:
1. ALTERNATIVES: If a recipe lists an alternative ingredient (e.g., "śmietanka lub mleko kokosowe"), put the first one in "food.name" ("śmietanka") and the other in "note" ("jako alternatywa może być mleko kokosowe").
2. GROUPED INGREDIENTS: If ingredients are grouped (e.g., "przyprawy (papryka, sól, pieprz)"), do NOT create one entry called "przyprawy". Instead, create THREE separate ingredient entries for "papryka", "sól", and "pieprz".
3. NO PLURAL: Use singular form for ingredient names where possible.
4. CLEAN NAMES: Remove descriptive words, states or adjectives from names and put them in "note" (e.g., "food: banany", "note: dojrzałe"; "food: sok z pomarańczy", "note: świeżo wyciśnięty"; "food: natka pietruszki", "note: świeża"; "food: cebula", "note: drobno posiekana").
5. UNIT EXTRACTION: If a name contains a natural unit (e.g., "ząbek czosnku", "puszka pomidorów"), move the unit to the "unit" field and leave only the product in "food.name" (e.g., "food: czosnek", "unit: ząbek").
6. NO BRANDS: Remove brand names or quality grades (e.g., "Mąka Szymanowska" -> "mąka pszenna"; "Masło Extra" -> "masło").
7. TEMPERATURE: Move temperature information to "note" (e.g., "food: masło", "note: zimne"; "food: woda", "note: ciepła").
8. SIZE: Move size adjectives to "note" (e.g., "food: jajko", "note: duże (L)"; "food: cebula", "note: mała").
9. NOUN FIRST: Format names as "Noun + Adjective" for better sorting (e.g., "czerwona papryka" -> "papryka czerwona"; "wędzony boczek" -> "boczek wędzony").

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

	rawStr := fullResponse.String()
	jsonStr := extractJSON(rawStr)

	return s.parseRecipesJSON(jsonStr, rawStr, correlationID)
}

func (s *GeminiService) ProcessRecipeFromImages(ctx context.Context, images [][]byte, mimeTypes []string, targetLanguage string, correlationID string) ([]*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Starting AI processing of %d images (Target Language: %s)", len(images), targetLanguage), "INFO")
	model := s.Client.GenerativeModel("gemini-3.1-pro-preview")

	// Force JSON output
	model.ResponseMIMEType = "application/json"

	if targetLanguage == "" {
		targetLanguage = "Polish"
	}

	promptParts := make([]genai.Part, 0, len(images)+1)

	// Append each image as ImageData part
	for i, imgData := range images {
		mime := "image/jpeg"
		if i < len(mimeTypes) && mimeTypes[i] != "" {
			mime = mimeTypes[i]
		}
		format := strings.TrimPrefix(mime, "image/")
		// Just to be safe, if format has parameters or is invalid, use jpeg
		if format == "" || strings.Contains(format, "/") {
			format = "jpeg"
		}
		promptParts = append(promptParts, genai.ImageData(format, imgData))
	}

	promptText := fmt.Sprintf(`
Analyze the provided images of culinary recipes. They may be photos of a cookbook, screenshots, or food photos.
A single source can contain multiple recipes. Extract all recipes from these images.

If the images do NOT contain any recipes (e.g., they are random photos, ads, etc.), return an empty JSON object: {}

IMPORTANT: All recipes, including their names, descriptions, step names, instructions, and ingredient names/notes MUST be in the following language: %s. If the source text in the images is in a different language, translate it accurately to %s.

DISH IMAGE SELECTION:
Look at all the uploaded images.
For each extracted recipe, identify the 0-based index of the image (from the uploaded list) that contains the photo of the finished dish/meal (the final result) for that specific recipe.
If one of the images is indeed a photo of the finished dish for a recipe, put its 0-based index in the "dish_image_index" field for that recipe.
If none of the uploaded images represent the finished dish/meal for that recipe (e.g., they only contain text, ingredients lists, or preparation steps), set "dish_image_index" to -1.

Add keywords (tags) to each recipe. Choose appropriate ones from the list below or add your own if they fit (translate them to %s as well):
vegan, vegetarian, for breakfast, for dinner, for lunch, snacks, for grill, coffee, drink, tea, smoothie, Polish cuisine, Japanese cuisine, Korean cuisine, Chinese cuisine, Sichuan cuisine, alcoholic drink, non-alcoholic drink, pancakes, cakes, soup, cream soup, bread, Italian cuisine, pasta, cheesecake, cake, salad.

Return the result as a strictly formatted JSON object matching the structure below:
{
  "recipes": [
    {
      "name": "Recipe Name (in %s)",
      "description": "Short description (in %s)",
      "working_time": preparation time in minutes (int),
      "waiting_time": waiting time in minutes (int),
      "servings": number of servings (int),
      "keywords": ["tag1", "tag2"],
      "dish_image_index": 0-based index of the finished dish photo (int, or -1 if none),
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
  ]
}

INGREDIENT EXTRACTION RULES:
1. ALTERNATIVES: If a recipe lists an alternative ingredient (e.g., "śmietanka lub mleko kokosowe"), put the first one in "food.name" ("śmietanka") and the other in "note" ("jako alternatywa może być mleko kokosowe").
2. GROUPED INGREDIENTS: If ingredients are grouped (e.g., "przyprawy (papryka, sól, pieprz)"), do NOT create one entry called "przyprawy". Instead, create THREE separate ingredient entries for "papryka", "sól", and "pieprz".
3. NO PLURAL: Use singular form for ingredient names where possible.
4. CLEAN NAMES: Remove descriptive words, states or adjectives from names and put them in "note" (e.g., "food: banany", "note: dojrzałe"; "food: sok z pomarańczy", "note: świeżo wyciśnięty"; "food: natka pietruszki", "note: świeża"; "food: cebula", "note: drobno posiekana").
5. UNIT EXTRACTION: If a name contains a natural unit (e.g., "ząbek czosnku", "puszka pomidorów"), move the unit to the "unit" field and leave only the product in "food.name" (e.g., "food: czosnek", "unit: ząbek").
6. NO BRANDS: Remove brand names or quality grades (e.g., "Mąka Szymanowska" -> "mąka pszenna"; "Masło Extra" -> "masło").
7. TEMPERATURE: Move temperature information to "note" (e.g., "food: masło", "note: zimne"; "food: woda", "note: ciepła").
8. SIZE: Move size adjectives to "note" (e.g., "food: jajko", "note: duże (L)"; "food: cebula", "note: mała").
9. NOUN FIRST: Format names as "Noun + Adjective" for better sorting (e.g., "czerwona papryka" -> "papryka czerwona"; "wędzony boczek" -> "boczek wędzony").
`, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage)

	promptParts = append(promptParts, genai.Text(promptText))

	resp, err := model.GenerateContent(ctx, promptParts...)
	if err != nil {
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Error generating content from images: %v", err), "ERROR")
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

	rawStr := fullResponse.String()
	jsonStr := extractJSON(rawStr)

	return s.parseRecipesJSON(jsonStr, rawStr, correlationID)
}

func (s *GeminiService) ProcessRecipeFromImagesAndText(ctx context.Context, images [][]byte, mimeTypes []string, text string, targetLanguage string, correlationID string) ([]*models.Recipe, error) {
	LogJSON(correlationID, "Gemini", fmt.Sprintf("Starting AI processing of %d images and text (Target Language: %s)", len(images), targetLanguage), "INFO")
	model := s.Client.GenerativeModel("gemini-3.1-pro-preview")

	// Force JSON output
	model.ResponseMIMEType = "application/json"

	if targetLanguage == "" {
		targetLanguage = "Polish"
	}

	promptParts := make([]genai.Part, 0, len(images)+1)

	// Append each image as ImageData part
	for i, imgData := range images {
		mime := "image/jpeg"
		if i < len(mimeTypes) && mimeTypes[i] != "" {
			mime = mimeTypes[i]
		}
		format := strings.TrimPrefix(mime, "image/")
		if format == "" || strings.Contains(format, "/") {
			format = "jpeg"
		}
		promptParts = append(promptParts, genai.ImageData(format, imgData))
	}

	promptText := fmt.Sprintf(`
Analyze the provided images and the accompanying text of culinary recipes. They may be photos of a cookbook, screenshots, food photos, and/or pasted text containing ingredients, steps, description.
A single source can contain multiple recipes. Extract all recipes from these images and the text combined.

If the images and text do NOT contain any recipes, return an empty JSON object: {}

IMPORTANT: All recipes, including their names, descriptions, step names, instructions, and ingredient names/notes MUST be in the following language: %s. If the source text in the images or text is in a different language, translate it accurately to %s.

DISH IMAGE SELECTION:
Look at all the uploaded images.
For each extracted recipe, identify the 0-based index of the image (from the uploaded list) that contains the photo of the finished dish/meal (the final result) for that specific recipe.
If one of the images is indeed a photo of the finished dish for a recipe, put its 0-based index in the "dish_image_index" field for that recipe.
If none of the uploaded images represent the finished dish/meal for that recipe (e.g., they only contain text, ingredients lists, or preparation steps), set "dish_image_index" to -1.

Add keywords (tags) to each recipe. Choose appropriate ones from the list below or add your own if they fit (translate them to %s as well):
vegan, vegetarian, for breakfast, for dinner, for lunch, snacks, for grill, coffee, drink, tea, smoothie, Polish cuisine, Japanese cuisine, Korean cuisine, Chinese cuisine, Sichuan cuisine, alcoholic drink, non-alcoholic drink, pancakes, cakes, soup, cream soup, bread, Italian cuisine, pasta, cheesecake, cake, salad.

Return the result as a strictly formatted JSON object matching the structure below:
{
  "recipes": [
    {
      "name": "Recipe Name (in %s)",
      "description": "Short description (in %s)",
      "working_time": preparation time in minutes (int),
      "waiting_time": waiting time in minutes (int),
      "servings": number of servings (int),
      "keywords": ["tag1", "tag2"],
      "dish_image_index": 0-based index of the finished dish photo (int, or -1 if none),
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
  ]
}

INGREDIENT EXTRACTION RULES:
1. ALTERNATIVES: If a recipe lists an alternative ingredient (e.g., "śmietanka lub mleko kokosowe"), put the first one in "food.name" ("śmietanka") and the other in "note" ("jako alternatywa może być mleko kokosowe").
2. GROUPED INGREDIENTS: If ingredients are grouped (e.g., "przyprawy (papryka, sól, pieprz)"), do NOT create one entry called "przyprawy". Instead, create THREE separate ingredient entries for "papryka", "sól", and "pieprz".
3. NO PLURAL: Use singular form for ingredient names where possible.
4. CLEAN NAMES: Remove descriptive words, states or adjectives from names and put them in "note" (e.g., "food: banany", "note: dojrzałe"; "food: sok z pomarańczy", "note: świeżo wyciśnięty"; "food: natka pietruszki", "note: świeża"; "food: cebula", "note: drobno posiekana").
5. UNIT EXTRACTION: If a name contains a natural unit (e.g., "ząbek czosnku", "puszka pomidorów"), move the unit to the "unit" field and leave only the product in "food.name" (e.g., "food: czosnek", "unit: ząbek").
6. NO BRANDS: Remove brand names or quality grades (e.g., "Mąka Szymanowska" -> "mąka pszenna"; "Masło Extra" -> "masło").
7. TEMPERATURE: Move temperature information to "note" (e.g., "food: masło", "note: zimne"; "food: woda", "note: ciepła").
8. SIZE: Move size adjectives to "note" (e.g., "food: jajko", "note: duże (L)"; "food: cebula", "note: mała").
9. NOUN FIRST: Format names as "Noun + Adjective" for better sorting (e.g., "czerwona papryka" -> "papryka czerwona"; "wędzony boczek" -> "boczek wędzony").

Accompanying text to process:
%s
`, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, targetLanguage, text)

	promptParts = append(promptParts, genai.Text(promptText))

	resp, err := model.GenerateContent(ctx, promptParts...)
	if err != nil {
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Error generating content from images and text: %v", err), "ERROR")
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

	rawStr := fullResponse.String()
	jsonStr := extractJSON(rawStr)

	return s.parseRecipesJSON(jsonStr, rawStr, correlationID)
}


type GeminiResponseWrapper struct {
	Recipes []*models.Recipe `json:"recipes"`
}

func (s *GeminiService) parseRecipesJSON(jsonStr string, rawStr string, correlationID string) ([]*models.Recipe, error) {
	// Check if it's empty
	if jsonStr == "{}" || jsonStr == "" {
		LogJSON(correlationID, "Gemini", "JSON is empty, skipping", "INFO")
		return nil, nil
	}

	// 1. Try to unmarshal into the wrapper struct
	var wrapper GeminiResponseWrapper
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err == nil && len(wrapper.Recipes) > 0 {
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Successfully unmarshaled %d recipes from wrapper format", len(wrapper.Recipes)), "INFO")
		var validRecipes []*models.Recipe
		for _, r := range wrapper.Recipes {
			if r != nil && r.Name != "" {
				validRecipes = append(validRecipes, r)
			}
		}
		return validRecipes, nil
	}

	// 2. Try to unmarshal directly as an array of recipes
	var recipesSlice []*models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &recipesSlice); err == nil && len(recipesSlice) > 0 {
		LogJSON(correlationID, "Gemini", fmt.Sprintf("Successfully unmarshaled %d recipes from array format", len(recipesSlice)), "INFO")
		var validRecipes []*models.Recipe
		for _, r := range recipesSlice {
			if r != nil && r.Name != "" {
				validRecipes = append(validRecipes, r)
			}
		}
		return validRecipes, nil
	}

	// 3. Try to unmarshal as a single recipe
	var singleRecipe models.Recipe
	if err := json.Unmarshal([]byte(jsonStr), &singleRecipe); err == nil {
		if singleRecipe.Name != "" {
			LogJSON(correlationID, "Gemini", "Successfully unmarshaled 1 recipe from single format", "INFO")
			return []*models.Recipe{&singleRecipe}, nil
		}
	}

	LogJSON(correlationID, "Gemini", fmt.Sprintf("Failed to unmarshal JSON into any known recipe format. Raw: %s", rawStr), "ERROR")
	return nil, fmt.Errorf("failed to unmarshal gemini response")
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Find first occurrence of { or [
	start := strings.IndexAny(s, "{[")
	if start == -1 {
		return s
	}

	// Find last occurrence of } or ]
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
