# Recipe Importer AI

A Go-based service that automatically scrapes recipes from social media (Instagram, YouTube, Facebook) and websites, processes them using Google Gemini AI into structured data, and saves them to your Tandoor Recipe Manager. Exposes a **Model Context Protocol (MCP)** server over SSE, allowing any MCP-compatible LLM client (e.g. Claude Desktop, Cursor) to import recipes directly via chat.

## Features

- **Multi-Source Scraping:** Support for Instagram (posts & profiles), YouTube (videos & shorts), Facebook, and general websites via Apify.
- **AI-Powered Processing:** Uses Google Gemini to extract ingredients, steps, servings, and metadata.
- **Auto-Translation:** Automatically translates any source recipe into the target language (default: **Polish**).
- **Keyword Support:** AI automatically assigns keywords (tags) like "vegan", "dinner", "asian cuisine" based on content.
- **Tandoor Integration:**
  - Support for multiple **Spaces**.
  - Automatic creation of missing ingredients and units.
  - Multipart image upload support.
  - Duplicate prevention based on Source URL.
- **Batch Processing:** Import all recipes from an Instagram profile or a text file via CLI.
- **Resilience:** Implemented retries with exponential backoff for Tandoor API (500/504 errors).
- **Web UI & CLI:** Simple web interface for single/profile imports and a CLI for bulk processing.
- **MCP Server:** Exposes an SSE-based MCP server allowing LLM agents to import and manage recipes programmatically.

## Prerequisites

- Go 1.22+ (if running locally)
- Docker & Docker Compose
- API Keys:
  - [Apify API Token](https://console.apify.com/account/integrations)
  - [Google Gemini API Key](https://aistudio.google.com/app/apikey)
  - [Tandoor Recipe Manager](https://tandoor.dev/) instance

## Configuration

Create a `.env` file in the root directory (see `.env.example`):

```env
APIFY_KEY=your_apify_key
GEMINI_KEY=your_gemini_key
TANDOOR_URL=https://your-tandoor.com
PORT=8080
# Optional: override base URL advertised to MCP clients (defaults to http://localhost:<PORT>)
# MCP_BASE_URL=https://your-public-domain.com
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

To import URLs from a text file (one URL per line, `#` for comments):
```bash
./recipe_importer_ai --file urls.txt --space 1 --token <your_tandoor_bearer_token>
```

Flags:
- `--file` – path to the text file with URLs
- `--space` – Tandoor Space ID
- `--token` – Tandoor bearer token (**required** in CLI mode)
- `--lang` – target language (default: `Polish`)

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Web UI |
| `GET` | `/imports` | Import history |
| `GET` | `/api/spaces` | List available Tandoor spaces |
| `POST` | `/api/login` | Authenticate and set session cookie |
| `POST` | `/api/logout` | Clear session |
| `GET` | `/import?url=<url>&space=<id>` | Trigger URL import (async) |
| `POST` | `/import-custom` | Import from text and/or images |
| `GET` | `/import/:id` | View import progress page |
| `DELETE` | `/api/recipe/:id` | Delete a recipe |

All API endpoints accept the Tandoor token either via the `Authorization: Bearer <token>` header or the `tandoor_token` session cookie.

## MCP Server (SSE Mode)

The application exposes a **Model Context Protocol** server over SSE, compatible with any MCP client (Claude Desktop, Cursor, etc.).

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/sse` | SSE connection — clients connect here |
| `POST` | `/message` | MCP JSON-RPC message endpoint |

### Authentication

Every request to the MCP server must include the Tandoor token via:
- `Authorization: Bearer <token>` header, **or**
- `X-Tandoor-Token: <token>` header

### Available Tools

| Tool | Description |
|------|-------------|
| `list_spaces` | List all Tandoor spaces with their IDs |
| `import_recipe_from_url` | Import a recipe from a URL (YouTube, Instagram, Facebook, websites) |
| `import_recipe_from_text` | Parse raw recipe text and import it into Tandoor |
| `get_import_status` | Check the status and logs of an import by `correlation_id` |
| `delete_recipe` | Delete a recipe from Tandoor by ID |

> **Tip:** When importing from an image or PDF, let the LLM perform OCR first and pass the extracted text to `import_recipe_from_text`.

### Example Client Configuration (Claude Desktop)

```json
{
  "mcpServers": {
    "recipe-importer": {
      "url": "http://localhost:8080/sse",
      "headers": {
        "Authorization": "Bearer <your_tandoor_bearer_token>"
      }
    }
  }
}
```

## License

MIT
