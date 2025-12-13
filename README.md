# 🌐 Screenshot
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

## How it works

1. **Request Processing**:
   - Validates the URL and checks for bot requests
   - Uses a headless Chrome browser via go-rod
   - Blocks unnecessary resources (ads, trackers, fonts, media) for faster loading
   - Captures the screenshot as JPEG

2. **Caching**:
   - Supports ETag-based browser caching
   - Cache TTL of 5 minutes (300 seconds)
   - Returns `304 Not Modified` for cached requests

3. **Performance Optimizations**:
   - Concurrent request limiting (max 10 simultaneous)
   - Blocks analytics, ads, and tracking scripts
   - Blocks fonts and media files for faster rendering
   - 30 second page timeout

## API Endpoints

### GET /

Captures a screenshot of the given URL.

**Parameters:**
- `url` (required): The URL to screenshot
- `preset` (optional): Dimension preset
  - `og` (default): 1200x630 (OpenGraph)
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
- `Content-Type`: image/jpeg
- `Cache-Control`: public, max-age=300
- `ETag`: Hash-based cache identifier
- `X-Setup-Ms`: Browser setup time
- `X-Nav-Ms`: Navigation time
- `X-Load-Ms`: Page load time
- `X-Screenshot-Ms`: Screenshot capture time
- `X-Total-Ms`: Total processing time

### GET /robots.txt

Returns robots.txt disallowing all crawlers.

### GET /healthz

Health check endpoint. Returns `ok` if the service is healthy.

## 📑 Docs

- See [DEVELOPMENT](./docs/development.md) for `development` guide.
- See [CONTRIBUTION](./docs/contribution.md) for `contribution` guide.

## 📜 License

Distributed under the MIT License © [wajeht](https://github.com/wajeht). See [LICENSE](./LICENSE) for more information.

