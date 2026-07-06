package gemini

import (
	"context"
	"fmt"
	"os"
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

func (s *GeminiService) GenerateJSON(ctx context.Context, modelName string, prompt string) (string, error) {
	model := s.Client.GenerativeModel(modelName)
	model.ResponseMIMEType = "application/json"

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("no response from Gemini")
	}

	var fullResponse strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		fullResponse.WriteString(fmt.Sprintf("%v", part))
	}
	return fullResponse.String(), nil
}

func (s *GeminiService) GenerateFromImages(ctx context.Context, modelName string, prompt string, images [][]byte, mimeTypes []string) (string, error) {
	model := s.Client.GenerativeModel(modelName)
	model.ResponseMIMEType = "application/json"

	promptParts := make([]genai.Part, 0, len(images)+1)
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
	promptParts = append(promptParts, genai.Text(prompt))

	resp, err := model.GenerateContent(ctx, promptParts...)
	if err != nil {
		return "", err
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("no response from Gemini")
	}

	var fullResponse strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		fullResponse.WriteString(fmt.Sprintf("%v", part))
	}
	return fullResponse.String(), nil
}

func (s *GeminiService) GenerateRawText(ctx context.Context, modelName string, prompt string) (string, error) {
	model := s.Client.GenerativeModel(modelName)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return "", fmt.Errorf("no response from Gemini")
	}

	var fullResponse strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		fullResponse.WriteString(fmt.Sprintf("%v", part))
	}
	return fullResponse.String(), nil
}
