package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestImportRecipeCustomHandler(t *testing.T) {
	_ = godotenv.Load("../.env")

	if os.Getenv("GEMINI_KEY") == "" || os.Getenv("TANDOOR_URL") == "" {
		t.Skip("Skipping test: GEMINI_KEY or TANDOOR_URL not set")
	}

	e := echo.New()

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	
	_ = writer.WriteField("space", "1")
	_ = writer.WriteField("lang", "English")
	_ = writer.WriteField("text", "Ingredients:\n1 cup milk\n2 eggs\n\nSteps:\n1. Mix milk and eggs.")

	part, err := writer.CreateFormFile("images", "test_recipe_image.jpg")
	assert.NoError(t, err)
	_, _ = part.Write([]byte("dummy image data"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/import-custom", body)
	req.Header.Set(echo.HeaderContentType, writer.FormDataContentType())
	req.Header.Set("X-Correlation-ID", "test-custom-cid")
	
	cookie := &http.Cookie{
		Name:  "tandoor_token",
		Value: "test-token",
	}
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h, err := setupTestHandler(context.Background())
	if err != nil {
		t.Skip("Gemini key is invalid/expired or could not initialize client")
	}

	err = h.ImportRecipeCustom(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, rec.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "Import started", resp["message"])
	assert.Equal(t, "test-custom-cid", resp["correlation_id"])
}
