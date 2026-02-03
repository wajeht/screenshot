FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o screenshot . && \
    ls -la /app/screenshot

FROM alpine:3.21

RUN apk --no-cache add \
    ca-certificates \
    sqlite \
    chromium \
    nss \
    freetype \
    harfbuzz

RUN addgroup -g 1000 -S screenshot && adduser -S screenshot -u 1000 -G screenshot

WORKDIR /app

RUN mkdir -p ./data && chown screenshot:screenshot ./data

COPY --chown=screenshot:screenshot --from=builder /app/screenshot ./screenshot

RUN ls -la /app/ && \
    chmod +x /app/screenshot

USER screenshot

ENV ROD_CHROME_PATH=/usr/bin/chromium-browser
ENV APP_ENV=production
ENV APP_PORT=80

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost/healthz || exit 1

CMD ["./screenshot"]
