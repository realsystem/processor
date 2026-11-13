# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o worker ./cmd/worker

# Runtime stage
FROM alpine:latest

# Install runtime dependencies and create non-root user
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 processor && \
    adduser -D -u 1000 -G processor processor

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/worker .
RUN chown processor:processor /app/worker

# Run as non-root user
USER processor

ENV CONTROL_PLANE_URL=http://control-plane:8080
ENV WORKER_ID=worker_docker
ENV POLL_INTERVAL=5s

CMD ["./worker"]
