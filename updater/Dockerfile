# Telegram updater bot: static binary, no CGO, no dependencies outside the Go standard library.
FROM golang:1.27-alpine AS builder
ARG VERSION=dev
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /updater-bot ./cmd/updater-bot

FROM alpine:latest
RUN apk --no-cache add ca-certificates && adduser -D -u 10001 updater
COPY --from=builder /updater-bot /usr/local/bin/updater-bot
USER 10001
ENTRYPOINT ["/usr/local/bin/updater-bot"]
