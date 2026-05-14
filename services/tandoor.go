package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"recipe_importer_ai/models"
	"time"
)

const (
	maxRetries    = 3
	retryInterval = 2 * time.Second
)

type TandoorService struct {
	BaseURL string
	Token   string
}

func NewTandoorService() *TandoorService {
	return &TandoorService{
		BaseURL: os.Getenv("TANDOOR_URL"),
		Token:   os.Getenv("TANDOOR_BEARER_TOKEN"),
	}
}

type PaginatedResponse struct {
	Results []map[string]interface{} `json:"results"`
}

type Space struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *TandoorService) GetSpaces(correlationID string) ([]Space, error) {
	return s.getWithRetry("/api/space/", "", correlationID)
}

func (s *TandoorService) getWithRetry(path string, spaceID string, correlationID string) ([]Space, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
			LogJSON(correlationID, "Tandoor", fmt.Sprintf("Retrying GET %s (attempt %d/%d)", path, i+1, maxRetries), "INFO")
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+s.Token)
		if spaceID != "" {
			req.Header.Set("X-Space-ID", spaceID)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(body))
			continue
		}
		defer resp.Body.Close()

		var data struct {
			Results []Space `json:"results"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}
		return data.Results, nil
	}
	return nil, lastErr
}

func (s *TandoorService) SaveRecipe(recipe *models.Recipe, spaceID string, correlationID string) error {
	LogJSON(correlationID, "Tandoor", fmt.Sprintf("Starting recipe save process for space %s: %s", spaceID, recipe.Name), "INFO")
	
	// 0. Check if recipe already exists
	exists, err := s.recipeExists(recipe.SourceURL, spaceID, correlationID)
	if err != nil {
		LogJSON(correlationID, "Tandoor", fmt.Sprintf("Error checking if recipe exists: %v", err), "ERROR")
		return err
	}
	if exists {
		LogJSON(correlationID, "Tandoor", fmt.Sprintf("Recipe from URL '%s' already exists in space %s, skipping", recipe.SourceURL, spaceID), "INFO")
		return nil
	}

	// 1. Process steps and ingredients to ensure everything exists
	for i, step := range recipe.Steps {
		LogJSON(correlationID, "Tandoor", fmt.Sprintf("Checking step %d: %s", i+1, step.Name), "INFO")
		for _, ing := range step.Ingredients {
			unitName := ing.Unit.Name
			if unitName == "" {
				unitName = "szt."
			}
			
			_, err := s.getOrCreateFood(ing.Food.Name, spaceID, correlationID)
			if err != nil {
				return err
			}
			_, err = s.getOrCreateUnit(unitName, spaceID, correlationID)
			if err != nil {
				return err
			}
		}
	}

    // 1b. Process keywords
    keywordObjs := []map[string]interface{}{}
    for _, kw := range recipe.Keywords {
        kid, err := s.getOrCreateKeyword(kw, spaceID, correlationID)
        if err == nil && kid > 0 {
            keywordObjs = append(keywordObjs, map[string]interface{}{"id": kid, "name": kw})
        } else {
            keywordObjs = append(keywordObjs, map[string]interface{}{"name": kw})
        }
    }

	// 2. Prepare Tandoor Recipe object
	tandoorRecipe := map[string]interface{}{
		"name":         recipe.Name,
		"description":  recipe.Description,
		"working_time": recipe.WorkingTime,
		"waiting_time": recipe.WaitingTime,
		"servings":     recipe.Servings,
		"source_url":   recipe.SourceURL,
		"steps":        s.mapSteps(recipe.Steps, spaceID, correlationID),
        "keywords":     keywordObjs,
	}

	LogJSON(correlationID, "Tandoor", "Sending final recipe to Tandoor API", "INFO")
	createdRecipe, err := s.postWithRetry("/api/recipe/", tandoorRecipe, spaceID, correlationID)
	if err != nil {
		LogJSON(correlationID, "Tandoor", fmt.Sprintf("Error saving recipe: %v", err), "ERROR")
		return err
	}

	recipeID := int(createdRecipe["id"].(float64))
	LogJSON(correlationID, "Tandoor", fmt.Sprintf("Recipe successfully created with ID: %d", recipeID), "INFO")

	if recipe.ImageURL != "" {
		LogJSON(correlationID, "Tandoor", fmt.Sprintf("Setting external image URL: %s", recipe.ImageURL), "INFO")
		err := s.updateImageMultipartWithRetry(recipeID, recipe.ImageURL, spaceID, correlationID)
		if err != nil {
			LogJSON(correlationID, "Tandoor", fmt.Sprintf("Warning: failed to set recipe image: %v", err), "WARN")
		}
	}

	return nil
}

func (s *TandoorService) postWithRetry(path string, body interface{}, spaceID string, correlationID string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
			LogJSON(correlationID, "Tandoor", fmt.Sprintf("Retrying POST %s (attempt %d/%d)", path, i+1, maxRetries), "INFO")
		}

		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", s.BaseURL+path, bytes.NewBuffer(b))
		req.Header.Set("Authorization", "Bearer "+s.Token)
		if spaceID != "" {
			req.Header.Set("X-Space-ID", spaceID)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(bodyBytes))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var res map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&res)
		return res, nil
	}
	return nil, lastErr
}

func (s *TandoorService) updateImageMultipartWithRetry(recipeID int, imageURL string, spaceID string, correlationID string) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("image_url", imageURL)
		writer.Close()

		path := fmt.Sprintf("/api/recipe/%d/image/", recipeID)
		req, _ := http.NewRequest("PUT", s.BaseURL+path, body)
		req.Header.Set("Authorization", "Bearer "+s.Token)
		if spaceID != "" {
			req.Header.Set("X-Space-ID", spaceID)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(bodyBytes))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
		}

		LogJSON(correlationID, "Tandoor", "Image URL successfully updated via multipart", "INFO")
		return nil
	}
	return lastErr
}

func (s *TandoorService) mapSteps(steps []models.Step, spaceID string, correlationID string) []map[string]interface{} {
	result := make([]map[string]interface{}, len(steps))
	for i, step := range steps {
		result[i] = map[string]interface{}{
			"name":        step.Name,
			"instruction": step.Instruction,
			"ingredients": s.mapIngredients(step.Ingredients, spaceID, correlationID),
		}
	}
	return result
}

func (s *TandoorService) mapIngredients(ingredients []models.Ingredient, spaceID string, correlationID string) []map[string]interface{} {
	result := make([]map[string]interface{}, len(ingredients))
	for i, ing := range ingredients {
		unitName := ing.Unit.Name
		if unitName == "" {
			unitName = "szt."
		}

		foodID, _ := s.getOrCreateFood(ing.Food.Name, spaceID, correlationID)
		unitID, _ := s.getOrCreateUnit(unitName, spaceID, correlationID)
		
		result[i] = map[string]interface{}{
			"food":   map[string]interface{}{"id": foodID, "name": ing.Food.Name},
			"unit":   map[string]interface{}{"id": unitID, "name": unitName},
			"amount": ing.Amount,
			"note":   ing.Note,
		}
	}
	return result
}

func (s *TandoorService) getOrCreateFood(name string, spaceID string, correlationID string) (int, error) {
    if name == "" { return 0, nil }
	results, err := s.getRawWithRetry("/api/food/?query="+url.QueryEscape(name), spaceID, correlationID)
	if err != nil {
		return 0, err
	}

	for _, res := range results {
		if stringsEqual(res["name"].(string), name) {
			return int(res["id"].(float64)), nil
		}
	}

	LogJSON(correlationID, "Tandoor", fmt.Sprintf("Food '%s' not found, creating new", name), "INFO")
	res, err := s.postWithRetry("/api/food/", map[string]interface{}{"name": name}, spaceID, correlationID)
	if err != nil {
		return 0, err
	}
	return int(res["id"].(float64)), nil
}

func (s *TandoorService) getOrCreateUnit(name string, spaceID string, correlationID string) (int, error) {
    if name == "" { return 0, nil }
	results, err := s.getRawWithRetry("/api/unit/?query="+url.QueryEscape(name), spaceID, correlationID)
	if err != nil {
		return 0, err
	}

	for _, res := range results {
		if stringsEqual(res["name"].(string), name) {
			return int(res["id"].(float64)), nil
		}
	}

	LogJSON(correlationID, "Tandoor", fmt.Sprintf("Unit '%s' not found, creating new", name), "INFO")
	res, err := s.postWithRetry("/api/unit/", map[string]interface{}{"name": name}, spaceID, correlationID)
	if err != nil {
		return 0, err
	}
	return int(res["id"].(float64)), nil
}

func (s *TandoorService) getOrCreateKeyword(name string, spaceID string, correlationID string) (int, error) {
    if name == "" { return 0, nil }
    results, err := s.getRawWithRetry("/api/keyword/?query="+url.QueryEscape(name), spaceID, correlationID)
    if err != nil {
        return 0, err
    }

    for _, res := range results {
        if stringsEqual(res["name"].(string), name) {
            return int(res["id"].(float64)), nil
        }
    }

    LogJSON(correlationID, "Tandoor", fmt.Sprintf("Keyword '%s' not found, creating new", name), "INFO")
    res, err := s.postWithRetry("/api/keyword/", map[string]interface{}{"name": name}, spaceID, correlationID)
    if err != nil {
        return 0, err
    }
    return int(res["id"].(float64)), nil
}

func stringsEqual(a, b string) bool {
	return url.QueryEscape(a) == url.QueryEscape(b)
}

func (s *TandoorService) getRawWithRetry(path string, spaceID string, correlationID string) ([]map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+s.Token)
		if spaceID != "" {
			req.Header.Set("X-Space-ID", spaceID)
		}
		
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 500 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(bodyBytes))
			continue
		}
		defer resp.Body.Close()

		var data PaginatedResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}
		return data.Results, nil
	}
	return nil, lastErr
}

func (s *TandoorService) recipeExists(sourceURL, spaceID, correlationID string) (bool, error) {
	if sourceURL == "" {
		return false, nil
	}

	results, err := s.getRawWithRetry("/api/recipe/?query="+url.QueryEscape(sourceURL), spaceID, correlationID)
	if err != nil {
		return false, err
	}

	for _, res := range results {
		existingURL, _ := res["source_url"].(string)
		if existingURL == sourceURL {
			return true, nil
		}
	}

	return false, nil
}
