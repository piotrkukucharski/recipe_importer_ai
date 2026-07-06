package tandoor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"recipe_importer_ai/infrastructure/logger"
	"recipe_importer_ai/models"
	"strings"
	"time"
)

const (
	maxRetries    = 3
	retryInterval = 2 * time.Second
)

type TandoorService struct {
	BaseURL string
}

func NewTandoorService() *TandoorService {
	return &TandoorService{
		BaseURL: os.Getenv("TANDOOR_URL"),
	}
}

type PaginatedResponse struct {
	Results []map[string]interface{} `json:"results"`
}

type Space struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (s *TandoorService) Authenticate(username, password string) (string, error) {
	url := s.BaseURL + "/api-token-auth/"
	body, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("invalid credentials")
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Token, nil
}

func (s *TandoorService) GetSpaces(token string, correlationID string) ([]Space, error) {
	return s.getWithRetry("/api/space/", "", token, correlationID)
}

func (s *TandoorService) switchSpace(spaceID string, token string, correlationID string) error {
	if spaceID == "" {
		return nil
	}

	path := fmt.Sprintf("/api/switch-active-space/%s/", spaceID)
	req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to switch space to %s: %s", spaceID, string(bodyBytes))
	}

	return nil
}

func (s *TandoorService) getWithRetry(path string, spaceID string, token string, correlationID string) ([]Space, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
			logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Retrying GET %s (attempt %d/%d)", path, i+1, maxRetries), "INFO")
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)

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

func (s *TandoorService) SaveRecipe(recipe *models.Recipe, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Starting recipe save process for space %s: %s", spaceID, recipe.Name), "INFO")
	
	// 0. Check if recipe already exists
	exists, err := s.recipeExists(recipe.SourceURL, spaceID, token, correlationID)
	if err != nil {
		logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Error checking if recipe exists: %v", err), "ERROR")
		return nil, err
	}
	if exists {
		logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Recipe from URL '%s' already exists in space %s, skipping", recipe.SourceURL, spaceID), "INFO")
		return nil, nil
	}

	// 1. Process steps and ingredients to ensure everything exists
	for i, step := range recipe.Steps {
		logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Checking step %d: %s", i+1, step.Name), "INFO")
		for _, ing := range step.Ingredients {
			unitName := ing.Unit.Name
			if unitName == "" {
				unitName = "szt."
			}
			
			_, err := s.getOrCreateFood(ing.Food.Name, spaceID, token, correlationID)
			if err != nil {
				return nil, err
			}
			_, err = s.getOrCreateUnit(unitName, spaceID, token, correlationID)
			if err != nil {
				return nil, err
			}
		}
	}

	// 1b. Process keywords
	keywordObjs := []map[string]interface{}{}
	for _, kw := range recipe.Keywords {
		kid, err := s.getOrCreateKeyword(kw, spaceID, token, correlationID)
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
		"steps":        s.mapSteps(recipe.Steps, spaceID, token, correlationID),
		"keywords":     keywordObjs,
		"image_url":    recipe.ImageURL,
	}

	logger.LogJSON(correlationID, "Tandoor", "Sending final recipe to Tandoor API", "INFO")
	createdRecipe, err := s.postWithRetry("/api/recipe/", tandoorRecipe, spaceID, token, correlationID)
	if err != nil {
		logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Error saving recipe: %v", err), "ERROR")
		return nil, err
	}

	recipeID := int(createdRecipe["id"].(float64))
	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Recipe successfully created with ID: %d", recipeID), "INFO")

	if recipe.ImageURL != "" {
		logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Setting external image URL: %s", recipe.ImageURL), "INFO")
		err := s.updateImageMultipartWithRetry(recipeID, recipe.ImageURL, spaceID, token, correlationID)
		if err != nil {
			logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Warning: failed to set recipe image: %v", err), "WARN")
		}
	}

	return createdRecipe, nil
}

func (s *TandoorService) postWithRetry(path string, body interface{}, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
			logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Retrying POST %s (attempt %d/%d)", path, i+1, maxRetries), "INFO")
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		b, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", s.BaseURL+path, bytes.NewBuffer(b))
		req.Header.Set("Authorization", "Bearer "+token)
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

func (s *TandoorService) updateImageMultipartWithRetry(recipeID int, imageURL string, spaceID string, token string, correlationID string) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("image_url", imageURL)
		writer.Close()

		path := fmt.Sprintf("/api/recipe/%d/image/", recipeID)
		req, _ := http.NewRequest("PUT", s.BaseURL+path, body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
			logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Failed to update image: %v", lastErr), "ERROR")
			continue
		}
		defer resp.Body.Close()

		logger.LogJSON(correlationID, "Tandoor", "Image URL successfully updated via multipart", "INFO")
		return nil
	}
	return lastErr
}

func (s *TandoorService) UpdateImageFileMultipartWithRetry(recipeID int, imgData []byte, mimeType string, spaceID string, token string, correlationID string) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		ext := ".jpg"
		if strings.Contains(mimeType, "png") {
			ext = ".png"
		} else if strings.Contains(mimeType, "webp") {
			ext = ".webp"
		} else if strings.Contains(mimeType, "gif") {
			ext = ".gif"
		}
		filename := fmt.Sprintf("recipe_image_%d%s", recipeID, ext)

		part, err := writer.CreateFormFile("image", filename)
		if err != nil {
			lastErr = err
			continue
		}
		_, err = part.Write(imgData)
		if err != nil {
			lastErr = err
			continue
		}

		writer.Close()

		path := fmt.Sprintf("/api/recipe/%d/image/", recipeID)
		req, _ := http.NewRequest("PUT", s.BaseURL+path, body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
			logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Failed to update image file: %v", lastErr), "ERROR")
			continue
		}
		defer resp.Body.Close()

		logger.LogJSON(correlationID, "Tandoor", "Image file successfully uploaded via multipart", "INFO")
		return nil
	}
	return lastErr
}

func (s *TandoorService) mapSteps(steps []models.Step, spaceID string, token string, correlationID string) []map[string]interface{} {
	result := make([]map[string]interface{}, len(steps))
	for i, step := range steps {
		result[i] = map[string]interface{}{
			"name":        step.Name,
			"instruction": step.Instruction,
			"ingredients": s.mapIngredients(step.Ingredients, spaceID, token, correlationID),
		}
	}
	return result
}

func (s *TandoorService) mapIngredients(ingredients []models.Ingredient, spaceID string, token string, correlationID string) []map[string]interface{} {
	result := make([]map[string]interface{}, len(ingredients))
	for i, ing := range ingredients {
		unitName := ing.Unit.Name
		if unitName == "" {
			unitName = "szt."
		}

		foodID, _ := s.getOrCreateFood(ing.Food.Name, spaceID, token, correlationID)
		unitID, _ := s.getOrCreateUnit(unitName, spaceID, token, correlationID)
		
		result[i] = map[string]interface{}{
			"food":   map[string]interface{}{"id": foodID, "name": ing.Food.Name},
			"unit":   map[string]interface{}{"id": unitID, "name": unitName},
			"amount": ing.Amount,
			"note":   ing.Note,
		}
	}
	return result
}

func (s *TandoorService) getOrCreateFood(name string, spaceID string, token string, correlationID string) (int, error) {
	if name == "" { return 0, nil }
	results, err := s.getRawWithRetry("/api/food/?query="+url.QueryEscape(name), spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}

	for _, res := range results {
		if stringsEqual(res["name"].(string), name) {
			return int(res["id"].(float64)), nil
		}
	}

	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Food '%s' not found, creating new", name), "INFO")
	res, err := s.postWithRetry("/api/food/", map[string]interface{}{"name": name}, spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}
	return int(res["id"].(float64)), nil
}

func (s *TandoorService) getOrCreateUnit(name string, spaceID string, token string, correlationID string) (int, error) {
	if name == "" { return 0, nil }
	results, err := s.getRawWithRetry("/api/unit/?query="+url.QueryEscape(name), spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}

	for _, res := range results {
		if stringsEqual(res["name"].(string), name) {
			return int(res["id"].(float64)), nil
		}
	}

	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Unit '%s' not found, creating new", name), "INFO")
	res, err := s.postWithRetry("/api/unit/", map[string]interface{}{"name": name}, spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}
	return int(res["id"].(float64)), nil
}

func (s *TandoorService) getOrCreateKeyword(name string, spaceID string, token string, correlationID string) (int, error) {
	if name == "" { return 0, nil }
	results, err := s.getRawWithRetry("/api/keyword/?query="+url.QueryEscape(name), spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}

	for _, res := range results {
		if stringsEqual(res["name"].(string), name) {
			return int(res["id"].(float64)), nil
		}
	}

	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Keyword '%s' not found, creating new", name), "INFO")
	res, err := s.postWithRetry("/api/keyword/", map[string]interface{}{"name": name}, spaceID, token, correlationID)
	if err != nil {
		return 0, err
	}
	return int(res["id"].(float64)), nil
}

func (s *TandoorService) DeleteRecipe(recipeID string, token string, correlationID string) error {
	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Requesting deletion of recipe ID: %s", recipeID), "INFO")
	
	path := fmt.Sprintf("/api/recipe/%s/", recipeID)
	req, _ := http.NewRequest("DELETE", s.BaseURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete recipe %s: %s", recipeID, string(bodyBytes))
	}

	logger.LogJSON(correlationID, "Tandoor", fmt.Sprintf("Recipe %s successfully deleted", recipeID), "INFO")
	return nil
}

func stringsEqual(a, b string) bool {
	return url.QueryEscape(a) == url.QueryEscape(b)
}

func (s *TandoorService) getRawWithRetry(path string, spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		
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

func (s *TandoorService) recipeExists(sourceURL, spaceID, token, correlationID string) (bool, error) {
	if sourceURL == "" {
		return false, nil
	}

	results, err := s.getRawWithRetry("/api/recipe/?query="+url.QueryEscape(sourceURL), spaceID, token, correlationID)
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

type PaginatedResponseWithNext struct {
	Results []map[string]interface{} `json:"results"`
	Next    string                   `json:"next"`
}

func (s *TandoorService) getRawWithPagination(path string, spaceID string, token string, correlationID string) ([]map[string]interface{}, string, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		
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
			return nil, "", fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var data PaginatedResponseWithNext
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, "", err
		}
		return data.Results, data.Next, nil
	}
	return nil, "", lastErr
}

func (s *TandoorService) getSingleWithRetry(path string, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		req, _ := http.NewRequest("GET", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		
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

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, lastErr
}

func (s *TandoorService) GetRecipes(spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	var allRecipes []map[string]interface{}
	path := "/api/recipe/?page_size=200"

	for path != "" {
		results, nextURL, err := s.getRawWithPagination(path, spaceID, token, correlationID)
		if err != nil {
			return nil, err
		}
		allRecipes = append(allRecipes, results...)
		
		if nextURL != "" {
			u, err := url.Parse(nextURL)
			if err != nil {
				break
			}
			path = u.RequestURI()
		} else {
			path = ""
		}
	}

	return allRecipes, nil
}

func (s *TandoorService) GetRecipeBooks(spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	path := "/api/recipe-book/?page_size=200"
	results, _, err := s.getRawWithPagination(path, spaceID, token, correlationID)
	return results, err
}

func (s *TandoorService) GetRecipeBook(bookID int, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/recipe-book/%d/", bookID)
	return s.getSingleWithRetry(path, spaceID, token, correlationID)
}

func (s *TandoorService) GetRecipeBookEntries(bookID int, spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	path := fmt.Sprintf("/api/recipe-book-entry/?book=%d&page_size=500", bookID)
	results, _, err := s.getRawWithPagination(path, spaceID, token, correlationID)
	return results, err
}

func (s *TandoorService) AddRecipeToBook(bookID int, recipeID int, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"book":   bookID,
		"recipe": recipeID,
	}
	return s.postWithRetry("/api/recipe-book-entry/", body, spaceID, token, correlationID)
}

func (s *TandoorService) PostWithRetry(path string, body interface{}, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	return s.postWithRetry(path, body, spaceID, token, correlationID)
}

func (s *TandoorService) GetSingleWithRetry(path string, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	return s.getSingleWithRetry(path, spaceID, token, correlationID)
}

func (s *TandoorService) PatchWithRetry(path string, body interface{}, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}

		req, _ := http.NewRequest("PATCH", s.BaseURL+path, bytes.NewBuffer(jsonBytes))
		req.Header.Set("Authorization", "Bearer "+token)
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

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, lastErr
}

func (s *TandoorService) PutWithRetry(path string, body interface{}, spaceID string, token string, correlationID string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		var jsonBytes []byte
		var err error
		if body != nil {
			jsonBytes, err = json.Marshal(body)
			if err != nil {
				return nil, err
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewBuffer(jsonBytes)
		}

		req, _ := http.NewRequest("PUT", s.BaseURL+path, bodyReader)
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
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

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var data map[string]interface{}
		bodyBytes, _ := io.ReadAll(resp.Body)
		if len(bodyBytes) > 0 {
			_ = json.Unmarshal(bodyBytes, &data)
		}
		return data, nil
	}
	return nil, lastErr
}

func (s *TandoorService) DeleteWithRetry(path string, spaceID string, token string, correlationID string) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(retryInterval * time.Duration(i))
		}

		if spaceID != "" {
			if err := s.switchSpace(spaceID, token, correlationID); err != nil {
				lastErr = err
				continue
			}
		}

		req, _ := http.NewRequest("DELETE", s.BaseURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)

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

		if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("tandoor error %d: %s", resp.StatusCode, string(bodyBytes))
		}
		return nil
	}
	return lastErr
}

func (s *TandoorService) GetKeywords(spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	var allKeywords []map[string]interface{}
	path := "/api/keyword/?page_size=200"

	for path != "" {
		results, nextURL, err := s.getRawWithPagination(path, spaceID, token, correlationID)
		if err != nil {
			return nil, err
		}
		allKeywords = append(allKeywords, results...)
		
		if nextURL != "" {
			u, err := url.Parse(nextURL)
			if err != nil {
				break
			}
			path = u.RequestURI()
		} else {
			path = ""
		}
	}
	return allKeywords, nil
}

func (s *TandoorService) GetAllItems(path string, spaceID string, token string, correlationID string) ([]map[string]interface{}, error) {
	var allItems []map[string]interface{}
	for path != "" {
		results, nextURL, err := s.getRawWithPagination(path, spaceID, token, correlationID)
		if err != nil {
			return nil, err
		}
		allItems = append(allItems, results...)

		if nextURL != "" {
			u, err := url.Parse(nextURL)
			if err != nil {
				break
			}
			path = u.RequestURI()
		} else {
			path = ""
		}
	}
	return allItems, nil
}
