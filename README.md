# 🖼️ Screenshot
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/wajeht/screenshot/blob/main/LICENSE) [![Open Source Love svg1](https://badges.frapsoft.com/os/v1/open-source.svg?v=103)](https://github.com/wajeht/screenshot)

automagically capture screenshots of any url

# 📖 Usage

add this to your html:

```html
<img loading="lazy" src="https://screenshot.jaw.dev?url=<url>" />
```

or with options:

```html
<img loading="lazy" src="https://screenshot.jaw.dev?url=<url>&preset=twitter" />
```

or preview on hover:
```html
<span class="preview">
  <a href="https://github.com/wajeht/screenshot">Github</a>
  <img
    class="preview-img"
    src="https://screenshot.jaw.dev?url=https://github.com/wajeht/screenshot"
    loading="lazy"
    alt="Preview"
  >
</span>

<style>
  .preview {
    position: relative;
  }

  .preview-img {
    position: absolute;
    top: 100%;
    left: 0;
    display: none;
    width: 300px;
    height: 158px;
    border: 1px solid #ccc;
    object-fit: cover;
    background: #fff;
  }

  .preview.loading::after {
    content: "Loading...";
    position: absolute;
    top: calc(100% + 70px);
    left: 110px;
    color: #666;
  }
</style>

<script>
  document.addEventListener("DOMContentLoaded", () => {
    const preview = document.querySelector(".preview");
    const img = preview.querySelector(".preview-img");

    preview.addEventListener("mouseenter", () => {
      img.style.display = "block";
      if (!img.complete) preview.classList.add("loading");
    });

    preview.addEventListener("mouseleave", () => {
      img.style.display = "none";
      preview.classList.remove("loading");
    });

    img.onload = () => {
      preview.classList.remove("loading");
    };
  });
</script>
```

## How it works

1. **Request Processing**:
   - Validates the URL and checks for bot requests
   - Uses a headless Chrome browser via go-rod
   - Blocks unnecessary resources (ads, trackers, fonts, media) for faster loading
   - Captures the screenshot as WebP

2. **Caching**:
   - Screenshots are cached in SQLite database
   - Subsequent requests for the same URL/dimensions are served from cache
   - Supports ETag-based browser caching
   - Cache TTL of 5 minutes (300 seconds)
   - Returns `304 Not Modified` for cached requests
   - `X-Cache: HIT` header indicates cache hit

3. **Performance Optimizations**:
   - Concurrent request limiting (max 10 simultaneous)
   - Blocks analytics, ads, and tracking scripts
   - Blocks fonts and media files for faster rendering
   - 30 second page timeout

## API Endpoints

### GET /

Without parameters, displays the documentation page. With `url` parameter, captures a screenshot.

**Parameters:**
- `url` (required): The URL to screenshot
- `preset` (optional): Dimension preset
  - `thumb` (default): 800x420
  - `og`: 1200x630 (OpenGraph)
  - `twitter`: 1200x675
  - `square`: 1080x1080
  - `mobile`: 375x667
  - `desktop`: 1920x1080
- `width` (optional): Custom width (max 1920)
- `height` (optional): Custom height (max 1920)
- `full` (optional): Set to `true` for full page screenshot

**Examples:**
```
https://screenshot.jaw.dev?url=github.com
https://screenshot.jaw.dev?url=github.com&preset=twitter
https://screenshot.jaw.dev?url=github.com&width=800&height=600
https://screenshot.jaw.dev?url=github.com&full=true
```

**Response Headers:**
- `Content-Type`: image/webp
- `Cache-Control`: public, max-age=300
- `ETag`: Hash-based cache identifier
- `X-Cache`: HIT (when served from database cache)
- `X-Setup-Ms`: Browser setup time
- `X-Nav-Ms`: Navigation time
- `X-Load-Ms`: Page load time
- `X-Screenshot-Ms`: Screenshot capture time
- `X-Total-Ms`: Total processing time

### GET /robots.txt

Returns robots.txt disallowing all crawlers.

### GET /healthz

Health check endpoint. Returns `ok` if the service is healthy.

### GET /blocked

Check if a domain is in the blocklist. Returns `blocked` or `allowed` as plain text.

**Parameters:**
- `domain` (required): The domain to check

**Examples:**
```
https://screenshot.jaw.dev/blocked?domain=doubleclick.net
# blocked

https://screenshot.jaw.dev/blocked?domain=google.com
# allowed
```

### GET /domains.json

Returns the full list of blocked domains as JSON array (~102k domains).

**Authentication:** Requires password.

### GET /screenshots

Displays a list of all cached screenshots. Returns an HTML table by default, or JSON with `?format=json`.

**Authentication:** Requires password.

**Parameters:**
- `format` (optional): Set to `json` for JSON response

**Examples:**
```
https://screenshot.jaw.dev/screenshots
# HTML table with preview images

https://screenshot.jaw.dev/screenshots?format=json
# JSON response
```

**JSON Response:**
```json
[
  {
    "id": 1,
    "url": "https://github.com",
    "data_size": 45678,
    "content_type": "image/webp",
    "width": 800,
    "height": 420,
    "created_at": "2025-01-15 10:30:00"
  }
]
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `APP_ENV` | Environment (`development` or `production`) | `development` |
| `APP_PORT` | Server port | `80` |
| `APP_PASSWORD` | Password for protected endpoints | Auto-generated |

If `APP_PASSWORD` is not set, a random 24-character password is generated on startup and logged to the console.

## 📑 Docs

- See [DEVELOPMENT](./docs/development.md) for `development` guide.
- See [CONTRIBUTION](./docs/contribution.md) for `contribution` guide.

## 📜 License

Distributed under the MIT License © [wajeht](https://github.com/wajeht). See [LICENSE](./LICENSE) for more information.
