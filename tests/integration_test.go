package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"recipe_importer_ai/api"
	"recipe_importer_ai/services"
	"testing"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// URL Recognition tests
func TestURLRecognition(t *testing.T) {
	apify := services.NewApifyService()

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
		actor, _ := apify.GetActorAndInput(tt.url)
		assert.Equal(t, tt.expected, actor)
	}
}

func TestInstagramProfileRecognition(t *testing.T) {
	apify := services.NewApifyService()

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
		assert.Equal(t, tt.expected, apify.IsInstagramProfile(tt.url))
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

	apify := services.NewApifyService()
	gemini, err := services.NewGeminiService(context.Background())
	if err != nil {
		t.Fatalf("Gemini initialization error: %v", err)
	}
	tandoor := services.NewTandoorService()

	h := &api.Handler{
		Apify:   apify,
		Gemini:  gemini,
		Tandoor: tandoor,
	}

	// 1. Scrape
	items, err := h.Apify.ScrapeItems(testURL, "test-cid")
	if err != nil {
		t.Fatalf("Scrape error: %v", err)
	}

	if len(items) > 0 {
		item := items[0]
		// 2. Process with Gemini
		recipe, err := h.Gemini.ProcessRecipe(context.Background(), item.Text, "Polish", "test-cid")
		if err != nil {
			t.Fatalf("Gemini error: %v", err)
		}
		if recipe != nil {
			recipe.SourceURL = testURL
			recipe.ImageURL = item.ImageURL

			// 3. Save to Tandoor
			if err := h.Tandoor.SaveRecipe(recipe, "", "test-cid"); err != nil {
				t.Fatalf("Tandoor error: %v", err)
			}
		}
	}
}
