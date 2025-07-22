FROM golang:1.24-alpine AS builder

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    freetype-dev \
    harfbuzz \
    ttf-freefont \
    fontconfig \
    ca-certificates \
    git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o screenshot

FROM alpine:latest

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ttf-freefont \
    fontconfig \
    ca-certificates

RUN adduser -D roduser
USER roduser

WORKDIR /app
COPY --from=builder /app/screenshot .

ENV ROD_CHROME_PATH=/usr/bin/chromium-browser

EXPOSE 80

CMD ["./screenshot"]

