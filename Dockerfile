FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -o screenshot . && \
    ls -la /app/screenshot

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

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
