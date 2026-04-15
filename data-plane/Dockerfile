# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 go build -o worker ./cmd/worker

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/worker .

ENV CONTROL_PLANE_URL=http://control-plane:8080
ENV WORKER_ID=worker_docker
ENV POLL_INTERVAL=5s

CMD ["./worker"]
