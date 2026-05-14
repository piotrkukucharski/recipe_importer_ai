package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type ApifyService struct {
	Token string
}

type ScrapedItem struct {
	Text     string
	ImageURL string
	URL      string
}

func NewApifyService() *ApifyService {
	return &ApifyService{
		Token: os.Getenv("APIFY_KEY"),
	}
}

func (s *ApifyService) Scrape(url string, correlationID string) (string, string, error) {
	items, err := s.ScrapeItems(url, correlationID)
	if err != nil {
		return "", "", err
	}
	if len(items) == 0 {
		return "", "", fmt.Errorf("no content found at URL")
	}
	
	// For backward compatibility/single URL, we return the first item or merged content
    // But since we want to handle profiles, we should probably use ScrapeItems in the handler.
    // For now, let's just return the first one if it's a single post.
	return items[0].Text, items[0].ImageURL, nil
}

func (s *ApifyService) ScrapeItems(url string, correlationID string) ([]ScrapedItem, error) {
	LogJSON(correlationID, "Apify", fmt.Sprintf("Starting scraping for URL: %s", url), "INFO")
	actorID, input := s.GetActorAndInput(url)
	if actorID == "" {
		LogJSON(correlationID, "Apify", "Unsupported URL format", "ERROR")
		return nil, fmt.Errorf("unsupported URL: %s", url)
	}

	LogJSON(correlationID, "Apify", fmt.Sprintf("Selected actor: %s", actorID), "INFO")
	apiUrl := fmt.Sprintf("https://api.apify.com/v2/acts/%s/run-sync-get-dataset-items?token=%s", actorID, s.Token)
	
	inputJson, _ := json.Marshal(input)
	resp, err := http.Post(apiUrl, "application/json", bytes.NewBuffer(inputJson))
	if err != nil {
		LogJSON(correlationID, "Apify", fmt.Sprintf("HTTP POST error: %v", err), "ERROR")
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		LogJSON(correlationID, "Apify", fmt.Sprintf("API returned error status %d: %s", resp.StatusCode, string(body)), "ERROR")
		return nil, fmt.Errorf("apify error: %s", string(body))
	}

	var results []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		LogJSON(correlationID, "Apify", fmt.Sprintf("Failed to decode response JSON: %v", err), "ERROR")
		return nil, err
	}

	LogJSON(correlationID, "Apify", fmt.Sprintf("Successfully retrieved %d items from dataset", len(results)), "INFO")

	var items []ScrapedItem
	for _, res := range results {
		item := ScrapedItem{}
		
		// Image extraction
		if img, ok := res["displayUrl"].(string); ok {
			item.ImageURL = img
		} else if img, ok := res["thumbnailUrl"].(string); ok {
			item.ImageURL = img
		} else if img, ok := res["imageUrl"].(string); ok {
			item.ImageURL = img
		}

		// URL extraction (for individual posts in profile)
		if u, ok := res["url"].(string); ok {
			item.URL = u
		} else if shortCode, ok := res["shortCode"].(string); ok {
			item.URL = "https://www.instagram.com/p/" + shortCode + "/"
		}

		// Text extraction
		var text strings.Builder
		if t, ok := res["text"].(string); ok {
			text.WriteString(t)
		} else if t, ok := res["fullText"].(string); ok {
			text.WriteString(t)
		} else if t, ok := res["description"].(string); ok {
			text.WriteString(t)
		} else if t, ok := res["caption"].(string); ok {
			text.WriteString(t)
		} else {
            b, _ := json.Marshal(res)
            text.WriteString(string(b))
        }
		item.Text = text.String()
		
		if item.Text != "" {
			items = append(items, item)
		}
	}

	return items, nil
}

func (s *ApifyService) GetActorAndInput(url string) (string, map[string]interface{}) {
	if strings.Contains(url, "youtube.com/shorts") || strings.Contains(url, "youtu.be/") && strings.Contains(url, "shorts") {
		return "streamers~youtube-shorts-scraper", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
	}
	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
		return "streamers~youtube-scraper", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
	}
	if strings.Contains(url, "instagram.com") {
        // Check if it's a profile or a post
        if s.IsInstagramProfile(url) {
            // resultsLimit=30 to not burn all credits but get recent posts
            return "apify~instagram-scraper", map[string]interface{}{
                "directUrls": []string{url},
                "resultsType": "posts",
                "resultsLimit": 20,
            }
        }
		return "apify~instagram-scraper", map[string]interface{}{"directUrls": []string{url}}
	}
	if strings.Contains(url, "facebook.com") {
		if strings.Contains(url, "/groups/") {
			return "apify~facebook-groups-scraper", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
		}
		if strings.Contains(url, "/posts/") || strings.Contains(url, "/permalink/") {
			return "apify~facebook-posts-scraper", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
		}
		return "apify~facebook-pages-scraper", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
	}

	return "apify~website-content-crawler", map[string]interface{}{"startUrls": []map[string]string{{"url": url}}}
}

func (s *ApifyService) IsInstagramProfile(url string) bool {
    // Basic check: profile doesn't have /p/ or /reels/ or /tv/
    // Example profile: https://www.instagram.com/kwestiasmakucom/
    // Example post: https://www.instagram.com/p/C6_.../
    return strings.Contains(url, "instagram.com") && 
           !strings.Contains(url, "/p/") && 
           !strings.Contains(url, "/reels/") && 
           !strings.Contains(url, "/reel/") && 
           !strings.Contains(url, "/tv/")
}
