# --- Stage 1: Build binary ---
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Cache Go modules
COPY go.mod ./
RUN go mod download

# Copy source files
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/snowflake ./cmd/snowflake

# --- Stage 2: Final Minimal Runtime Image ---
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary and static web assets
COPY --from=builder /app/snowflake /app/snowflake
COPY --from=builder /app/web /app/web

EXPOSE 8080

ENTRYPOINT ["/app/snowflake", "serve-saas", "--addr", ":8080"]
