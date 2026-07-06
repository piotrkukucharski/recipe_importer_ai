package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"recipe_importer_ai/infrastructure/apify"
	"testing"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// URL Recognition tests
func TestURLRecognition(t *testing.T) {
	apifySvc := apify.NewApifyService()

	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.youtube.com/watch?v=123", "streamers~youtube-scraper"},
		{"https://www.youtube.com/shorts/abc", "streamers~youtube-shorts-scraper"},
		{"https://www.instagram.com/p/xyz/", "apify~instagram-scraper"},
		{"https://www.facebook.com/groups/123", "apify~facebook-groups-scraper"},
		{"https://example.com/recipe", "apify~website-content-crawler"},
	}

	for _, tt := range tests {
		actor, _ := apifySvc.GetActorAndInput(tt.url)
		assert.Equal(t, tt.expected, actor)
	}
}

func TestInstagramProfileRecognition(t *testing.T) {
	apifySvc := apify.NewApifyService()

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.instagram.com/p/DKqN7RezxS5/", false},
		{"https://www.instagram.com/reel/C6_xyz/", false},
		{"https://www.instagram.com/kwestiasmakucom/", true},
		{"https://www.instagram.com/user123", true},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, apifySvc.IsInstagramProfile(tt.url))
	}
}

// Integration test requiring .env
func TestFullImportFlow(t *testing.T) {
	_ = godotenv.Load("../.env")

	if os.Getenv("APIFY_KEY") == "" || os.Getenv("TANDOOR_URL") == "" {
		t.Skip("Skipping integration test: Missing keys in .env")
	}

	e := echo.New()
	testURL := "https://www.youtube.com/shorts/m6M-0WInIuY" 
	req := httptest.NewRequest(http.MethodGet, "/import?url="+testURL, nil)
	rec := httptest.NewRecorder()
	_ = e.NewContext(req, rec)

	h, err := setupTestHandler(context.Background())
	if err != nil {
		t.Fatalf("setupTestHandler error: %v", err)
	}

	// 1. Scrape
	items, err := h.Tandoor.GetRecipes("", "", "") // Just placeholder or call apify directly
	_ = items
	itemsScraped, err := h.ImportURLUC.Apify.ScrapeItems(testURL, "test-cid")
	if err != nil {
		t.Fatalf("Scrape error: %v", err)
	}

	if len(itemsScraped) > 0 {
		item := itemsScraped[0]
		// 2. Process with Gemini via our processor
		recipes, err := h.ImportURLUC.Processor.ProcessRecipe(context.Background(), item.Text, item.Images, "Polish", false, "test-cid")
		if err != nil {
			t.Fatalf("Gemini error: %v", err)
		}
		for _, recipeObj := range recipes {
			recipeObj.SourceURL = testURL
			recipeObj.ImageURL = item.ImageURL

			// 3. Save to Tandoor
			if _, err := h.Tandoor.SaveRecipe(recipeObj, "", "test-token", "test-cid"); err != nil {
				t.Fatalf("Tandoor error: %v", err)
			}
		}
	}
}
