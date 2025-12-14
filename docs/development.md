# 💻 Development

## Prerequisites

- Go 1.25+
- Docker
- Make

## Quick Start

1. Clone the repository:
```bash
git clone https://github.com/wajeht/screenshot.git
cd screenshot
```

2. Start the development server:
```bash
make dev
```

This will build and run the Docker container with hot-reload enabled.

## Available Commands

| Command | Description |
|---------|-------------|
| `make dev` | Start development server with hot-reload |
| `make test` | Run tests |
| `make format` | Format Go code |
| `make clean` | Clean up Docker containers and database files |
| `make filters` | Regenerate blocklist from filter files |
| `make deploy` | Deploy to production (requires .env) |

## Project Structure

```
screenshot/
├── assets/
│   ├── embed.go           # Embedded filesystem
│   ├── filters/           # Ad/tracker blocklist files
│   ├── migrations/        # Database migrations
│   ├── static/            # Static assets (favicon, icons)
│   └── templates/         # HTML templates
├── data/                  # SQLite database (gitignored)
├── docs/                  # Documentation
├── main.go                # Main application
├── main_test.go           # Tests
├── filter_parser.go       # Blocklist parser
├── Dockerfile             # Production Docker image
├── Dockerfile.dev         # Development Docker image
└── Makefile               # Build commands
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_PORT` | `80` | Server port |
| `APP_ENV` | `development` | Environment (development/production) |

## Database

Screenshots are cached in SQLite at `./data/db.sqlite`. The database is created automatically on first run.

To reset the database:
```bash
make clean
```

## Updating Blocklist

The blocklist is generated from filter files in `assets/filters/`. To regenerate:

```bash
make filters
```

This parses EasyList and other ad-blocking filter lists into a JSON file.

## Testing

Run tests with:
```bash
make test
```

Or directly:
```bash
go test -v ./...
```
