# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Install build dependencies for SQLite
RUN apk add --no-cache gcc musl-dev

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with dynamic linking to properly support TLS certificate verification
RUN CGO_ENABLED=1 GOOS=linux go build -o server ./cmd/server

# Runtime stage
FROM alpine:latest

# Install runtime dependencies (including libc for dynamic linking)
RUN apk --no-cache add ca-certificates sqlite wget libc6-compat && \
    addgroup -g 1000 processor && \
    adduser -D -u 1000 -G processor processor && \
    mkdir -p /data && \
    chown processor:processor /data

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server .

# Copy templates and static files
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static

# Set ownership for processor user
RUN chown -R processor:processor /app

# Run as non-root user
USER processor

ENV DATABASE_PATH=/data/processor.db
ENV HTTP_PORT=8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/api/v1/health || exit 1

CMD ["./server"]
