# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Install build dependencies for SQLite
RUN apk add --no-cache gcc musl-dev

# Build binary
RUN CGO_ENABLED=1 go build -o server ./cmd/server

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/server .

# Create data directory
RUN mkdir -p /data

ENV DATABASE_PATH=/data/processor.db
ENV HTTP_PORT=8080

EXPOSE 8080

CMD ["./server"]
