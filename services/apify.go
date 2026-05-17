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
	Text       string
	ImageURL   string   // This will be the AI-selected image
	Images     []string // All potential images
	URL        string
	VideoURL   string
	Transcript string
}

func NewApifyService() *ApifyService {
	return &ApifyService{
		Token: os.Getenv("APIFY_KEY"),
	}
}

func (s *ApifyService) Scrape(url string, correlationID string) (string, string, []string, error) {
	items, err := s.ScrapeItems(url, correlationID)
	if err != nil {
		return "", "", nil, err
	}
	if len(items) == 0 {
		return "", "", nil, fmt.Errorf("no content found at URL")
	}
	
	return items[0].Text, items[0].ImageURL, items[0].Images, nil
}

func (s *ApifyService) ScrapeItems(url string, correlationID string) ([]ScrapedItem, error) {
	LogJSON(correlationID, "Apify", fmt.Sprintf("Starting scraping for URL: %s", url), "INFO")
	actorID, input := s.GetActorAndInput(url)
	if actorID == "" {
		LogJSON(correlationID, "Apify", "Unsupported URL format", "ERROR")
		return nil, fmt.Errorf("unsupported URL: %s", url)
	}

	LogJSON(correlationID, "Apify", fmt.Sprintf("Selected actor: %s", actorID), "INFO")
	apiUrl := fmt.Sprintf("https://api.apify.com/v2/acts/%s/run-sync-get-dataset-items?token=%s&timeout=120", actorID, s.Token)
	
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
		
		// Image extraction - collect ALL
		item.Images = s.findAllImages(res)
		if len(item.Images) > 0 {
			item.ImageURL = item.Images[0]
		}

		// URL extraction (for individual posts in profile)
		if u, ok := res["url"].(string); ok {
			item.URL = u
		} else if shortCode, ok := res["shortCode"].(string); ok {
			item.URL = "https://www.instagram.com/p/" + shortCode + "/"
		}

		// Video URL extraction
		if v, ok := res["videoUrl"].(string); ok {
			item.VideoURL = v
		}

		// Native Transcript extraction (YouTube)
		if t, ok := res["transcript"].(string); ok {
			item.Transcript = t
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
		return "streamers~youtube-shorts-scraper", map[string]interface{}{
            "startUrls": []map[string]string{{"url": url}},
            "maxConcurrency": 1,
        }
	}
	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") {
		return "streamers~youtube-scraper", map[string]interface{}{
            "startUrls": []map[string]string{{"url": url}},
            "maxConcurrency": 1,
        }
	}
	if strings.Contains(url, "instagram.com") {
        // Check if it's a profile or a post
        if s.IsInstagramProfile(url) {
            // resultsLimit=30 to not burn all credits but get recent posts
            return "apify~instagram-scraper", map[string]interface{}{
                "directUrls": []string{url},
                "resultsType": "posts",
                "resultsLimit": 20,
                "maxConcurrency": 1,
            }
        }
		return "apify~instagram-scraper", map[string]interface{}{
            "directUrls": []string{url},
            "maxConcurrency": 1,
        }
	}
	if strings.Contains(url, "facebook.com") {
		if strings.Contains(url, "/groups/") {
			return "apify~facebook-groups-scraper", map[string]interface{}{
                "startUrls": []map[string]string{{"url": url}},
                "maxConcurrency": 1,
            }
		}
		if strings.Contains(url, "/posts/") || strings.Contains(url, "/permalink/") {
			return "apify~facebook-posts-scraper", map[string]interface{}{
                "startUrls": []map[string]string{{"url": url}},
                "maxConcurrency": 1,
            }
		}
		return "apify~facebook-pages-scraper", map[string]interface{}{
            "startUrls": []map[string]string{{"url": url}},
            "maxConcurrency": 1,
        }
	}

	return "apify~website-content-crawler", map[string]interface{}{
        "startUrls": []map[string]string{{"url": url}},
        "maxConcurrency": 1,
    }
}

func (s *ApifyService) findAllImages(res map[string]interface{}) []string {
	var images []string
	seen := make(map[string]bool)

	// High priority keys
	priorityKeys := []string{"displayUrl", "thumbnailUrl", "imageUrl", "topImage", "image", "mainImage", "ogImage"}
	for _, key := range priorityKeys {
		if val, ok := res[key].(string); ok && val != "" && strings.HasPrefix(val, "http") {
			if !seen[val] {
				images = append(images, val)
				seen[val] = true
			}
		}
	}

	// Recursive search
	recursiveImgs := s.collectImagesFromMap(res)
	for _, img := range recursiveImgs {
		if !seen[img] {
			images = append(images, img)
			seen[img] = true
		}
	}

	return images
}

func (s *ApifyService) collectImagesFromMap(m map[string]interface{}) []string {
	var images []string
	
	for _, val := range m {
		switch v := val.(type) {
		case string:
			if strings.HasPrefix(v, "http") && (strings.Contains(v, ".jpg") || strings.Contains(v, ".png") || strings.Contains(v, ".webp") || strings.Contains(v, ".jpeg")) {
				images = append(images, v)
			}
		case map[string]interface{}:
			images = append(images, s.collectImagesFromMap(v)...)
		case []interface{}:
			for _, item := range v {
				if subMap, ok := item.(map[string]interface{}); ok {
					images = append(images, s.collectImagesFromMap(subMap)...)
				} else if s, ok := item.(string); ok && strings.HasPrefix(s, "http") {
					if strings.Contains(s, ".jpg") || strings.Contains(s, ".png") || strings.Contains(s, ".webp") || strings.Contains(s, ".jpeg") {
						images = append(images, s)
					}
				}
			}
		}
	}
	return images
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
