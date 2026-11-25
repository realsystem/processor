// Command telegram-bot is a Telegram front end for the Processor control plane. It runs as its
// own process, next to the control plane or on another machine, and talks to it only through
// the public API, so it can be started, stopped or replaced without touching the server.
//
//	TELEGRAM_BOT_TOKEN      token from @BotFather (required)
//	TELEGRAM_ALLOWED_USERS  comma-separated Telegram user ids allowed to chat (required)
//	PROCESSOR_API_KEY       control-plane API key (required; ./server -create-api-key <email>)
//	PROCESSOR_URL           control plane address (default http://localhost:8080)
//	BOT_DEFAULT_MODEL       model for chats that have not picked one (default: the control plane's)
//	BOT_JOB_TIMEOUT         how long to wait for one answer (default 15m)
//	BOT_SENSITIVE           "false" to stop marking jobs sensitive (default true)
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/processor/control-plane/internal/telegrambot"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	apiKey := os.Getenv("PROCESSOR_API_KEY")
	if token == "" || apiKey == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN and PROCESSOR_API_KEY are required")
	}
	allowed, err := telegrambot.ParseAllowedUsers(os.Getenv("TELEGRAM_ALLOWED_USERS"))
	if err != nil {
		log.Fatalf("TELEGRAM_ALLOWED_USERS: %v", err)
	}
	if len(allowed) == 0 {
		log.Fatal("TELEGRAM_ALLOWED_USERS is required: every message is a GPU job, so the bot is never open to everyone (message the bot to see your user id)")
	}
	timeout := 15 * time.Minute
	if v := os.Getenv("BOT_JOB_TIMEOUT"); v != "" {
		if timeout, err = time.ParseDuration(v); err != nil {
			log.Fatalf("BOT_JOB_TIMEOUT: %v", err)
		}
	}
	baseURL := os.Getenv("PROCESSOR_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	bot := telegrambot.New(
		telegrambot.NewTelegram(os.Getenv("TELEGRAM_API_URL"), token),
		telegrambot.NewProcessor(baseURL, apiKey),
		telegrambot.Config{
			Allowed:      allowed,
			DefaultModel: os.Getenv("BOT_DEFAULT_MODEL"),
			JobTimeout:   timeout,
			Sensitive:    os.Getenv("BOT_SENSITIVE") != "false",
		},
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("Telegram bot started: control plane %s, %d allowed user(s)", baseURL, len(allowed))
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Print("Telegram bot stopped")
}
