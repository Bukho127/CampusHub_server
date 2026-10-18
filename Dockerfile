# Build the configured Valkey image with --target valkey.
FROM valkey/valkey:9.1.2-alpine AS valkey

# Apply available security updates from the base image's Alpine repositories.
RUN apk upgrade --no-cache

COPY docker/valkey/valkey.conf /usr/local/etc/valkey/valkey.conf

CMD ["valkey-server", "/usr/local/etc/valkey/valkey.conf"]

# Build the Go API.
FROM golang:1.26.9-alpine AS builder

RUN apk upgrade --no-cache

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# Keep the API last so it remains the default build target.
FROM alpine:3.24 AS api

RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates ffmpeg \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/api ./api

USER app
EXPOSE 8080

CMD ["./api"]
