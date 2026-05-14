# Agent Documentation: Recipe Importer AI

## System Overview
Recipe Importer AI is an autonomous ingestion pipeline designed to bridge the gap between social media content and structured personal recipe databases (specifically Tandoor). It handles the complete lifecycle of a recipe import: from raw URL scraping and unstructured text extraction to AI-driven parsing, translation, and structured API synchronization.

## Architecture

### 1. Scraper Layer (`services/apify.go`)
- **Engine:** Apify API.
- **Routing:** Context-aware routing based on URL patterns.
- **Profiles:** Special handling for Instagram profiles. It identifies profile URLs and triggers a "posts" result type instead of a single "post" result.
- **Actors used:** 
  - `streamers/youtube-scraper`
  - `streamers/youtube-shorts-scraper`
  - `apify/instagram-scraper`
  - `apify/facebook-groups-scraper`
  - `apify/website-content-crawler`

### 2. AI Processing Layer (`services/gemini.go`)
- **Model:** `gemini-1.5-flash` (configured via `gemini-3-flash-preview` alias in some regions).
- **Core Logic:** A strict system prompt ensures that regardless of the source language, the output is always a structured JSON in **Polish**.
- **Extraction:** Captures ingredients (amounts, units, notes), instructions (split into steps), servings, times, and tags.
- **Validation:** Filters out non-recipe content by returning an empty JSON object `{}`.

### 3. Storage Layer (`services/tandoor.go`)
- **Target:** Tandoor Recipe Manager REST API.
- **Space Management:** Uses `X-Space-ID` header to route data to the correct user space.
- **Dependency Resolution:** Automatically looks up or creates Food, Units, and Keywords before saving the recipe to avoid foreign key errors.
- **Image Sync:** Performed via a separate `PUT` multipart request to `/api/recipe/{id}/image/` to ensure Tandoor handles the image URL correctly.
- **Duplicate Prevention:** Strict exact-match check on `source_url`.
- **Resilience:** Sequential processing for batch imports and exponential backoff retries for server-side errors (500/504).

## Critical Configuration

- **Correlation-ID:** Every log entry and request flow is tagged with `X-Correlation-ID` for end-to-end tracing.
- **Logging:** Structured JSON logging to `stdout`, compatible with CloudWatch/ELK/Docker logs.

## Maintenance Notes
- If Apify actors change their output schema, update the `ScrapedItem` mapping in `services/apify.go`.
- The `gemini-3-flash-preview` model name is used; if the API deprecates this, switch to `gemini-1.5-flash` or newer.
- Tandoor API behavior regarding keywords might vary; current implementation uses nested creation.

## Technical Stack
- **Language:** Go 1.22
- **Framework:** Echo v4 (Web Server)
- **Infrastructure:** Docker (Alpine base), Docker Compose
- **Key Libraries:** 
  - `github.com/labstack/echo/v4`
  - `github.com/google/generative-ai-go/genai`
  - `github.com/joho/godotenv`
