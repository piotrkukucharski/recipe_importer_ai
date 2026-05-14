# Recipe Importer AI

A Go-based service that automatically scrapes recipes from social media (Instagram, YouTube, Facebook) and websites, processes them using Google Gemini AI into structured data, and saves them to your Tandoor Recipe Manager.

## Features

- **Multi-Source Scraping:** Support for Instagram (posts & profiles), YouTube (videos & shorts), Facebook, and general websites via Apify.
- **AI-Powered Processing:** Uses Google Gemini (gemini-1.5-flash) to extract ingredients, steps, servings, and metadata.
- **Auto-Translation:** Automatically translates any source recipe into high-quality **Polish**.
- **Keyword Support:** AI automatically assigns keywords (tags) like "vegan", "dinner", "asian cuisine" based on content.
- **Tandoor Integration:**
  - Support for multiple **Spaces**.
  - Automatic creation of missing ingredients and units.
  - Multipart image upload support.
  - Duplicate prevention based on Source URL.
- **Batch Processing:** Import all recipes from an Instagram profile or a text file.
- **Resilience:** Implemented retries with exponential backoff for Tandoor API (500/504 errors).
- **Web UI & CLI:** Simple web interface for single/profile imports and a CLI for bulk processing.

## Prerequisites

- Go 1.22+ (if running locally)
- Docker & Docker Compose
- API Keys:
  - [Apify API Token](https://console.apify.com/account/integrations)
  - [Google Gemini API Key](https://aistudio.google.com/app/apikey)
  - [Tandoor Recipe Manager](https://tandoor.dev/) instance and Bearer Token

## Configuration

Create a `.env` file in the root directory (see `.env.example`):

```env
APIFY_KEY=your_apify_key
GEMINI_KEY=your_gemini_key
TANDOOR_URL=https://your-tandoor.com
TANDOOR_BEARER_TOKEN=your_tandoor_token
PORT=8080
```

## Running the Application

### Using Docker (Recommended)

```bash
docker-compose up -d --build
```
The Web UI will be available at `http://localhost:8080`.

### Running Locally

```bash
go run main.go
```

### CLI Batch Mode

To import URLs from a file:
```bash
./recipe_importer_ai --file urls.txt --space 1
```

## API Endpoints

- `GET /`: Web UI
- `GET /api/spaces`: List available Tandoor spaces
- `GET /import?url=<url>&space=<id>`: Trigger an import (asynchronous)

## License

MIT
