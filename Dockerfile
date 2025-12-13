FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o screenshot

FROM alpine:3.20

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    && adduser -D roduser

USER roduser

WORKDIR /app
COPY --from=builder /app/screenshot .

ENV ROD_CHROME_PATH=/usr/bin/chromium-browser

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:80/health || exit 1

CMD ["./screenshot"]

