// Command updater-bot is the Telegram updater for a processor control plane: a small outbound-only
// bot that accepts /update <code> from the owner and hands a validated request to the host.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/realsystem/processor/updater/internal/bot"
	"github.com/realsystem/processor/updater/internal/release"
	"github.com/realsystem/processor/updater/internal/state"
	"github.com/realsystem/processor/updater/internal/totp"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// secretFromEnvOrFile reads a secret from NAME_FILE (preferred) or NAME.
func secretFromEnvOrFile(name string) (string, error) {
	if path := env(name+"_FILE", ""); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s_FILE: %w", name, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return env(name, ""), nil
}

func parseIDs(s string) (map[int64]bool, error) {
	ids := map[int64]bool{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("UPDATER_ALLOWED_IDS: %q is not a Telegram user id", f)
		}
		ids[id] = true
	}
	return ids, nil
}

func totpSetup(botDir string, force bool) error {
	if err := os.MkdirAll(botDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", botDir, err)
	}
	path := filepath.Join(botDir, "totp.secret")
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists: not overwriting it (it is never shown again; use --force to replace it)", path)
	}
	secret, err := totp.GenerateSecret()
	if err != nil {
		return err
	}
	if err := state.WriteFileAtomic(path, []byte(secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("store the secret: %w", err)
	}
	account := env("UPDATER_TOTP_ACCOUNT", "processor")
	fmt.Println("TOTP secret (shown once; stored mode 600 in " + path + "):")
	fmt.Println("  " + secret)
	fmt.Println("otpauth URI (paste into an authenticator app, or make a QR code locally):")
	fmt.Println("  " + totp.URI(secret, "Processor updater", account))
	return nil
}

func main() {
	setup := flag.Bool("totp-setup", false, "create the owner's TOTP secret, print it once and exit")
	force := flag.Bool("force", false, "with --totp-setup: replace an existing secret")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	log.SetFlags(log.LstdFlags | log.LUTC)

	botDir := env("UPDATER_BOT_DIR", "/state/bot")
	if *setup {
		if err := totpSetup(botDir, *force); err != nil {
			log.Fatalf("totp setup: %v", err)
		}
		return
	}

	token, err := secretFromEnvOrFile("UPDATER_TELEGRAM_TOKEN")
	if err != nil {
		log.Fatal(err)
	}
	if token == "" {
		log.Fatal("UPDATER_TELEGRAM_TOKEN_FILE (or UPDATER_TELEGRAM_TOKEN) is required")
	}
	ghToken, err := secretFromEnvOrFile("UPDATER_GITHUB_TOKEN")
	if err != nil {
		log.Fatal(err)
	}
	ids, err := parseIDs(env("UPDATER_ALLOWED_IDS", ""))
	if err != nil {
		log.Fatal(err)
	}
	if len(ids) == 0 {
		log.Print("UPDATER_ALLOWED_IDS is empty: nobody can use the bot yet. Send /id to it, then add your id and restart.")
	}
	repo := env("UPDATER_REPO", "")
	if repo == "" || strings.Count(repo, "/") != 1 {
		log.Fatal("UPDATER_REPO must be set to owner/name")
	}
	requireCI := true
	if v := strings.ToLower(env("UPDATER_REQUIRE_CI", "true")); v == "false" || v == "0" || v == "no" {
		requireCI = false
		log.Print("UPDATER_REQUIRE_CI is off: releases are deployed without checking CI")
	}
	for _, d := range []string{botDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			log.Fatalf("create %s: %v", d, err)
		}
	}
	cfg := bot.Config{
		AllowedIDs:  ids,
		Repo:        repo,
		RequireCI:   requireCI,
		Version:     version,
		BotDir:      botDir,
		RequestsDir: env("UPDATER_REQUESTS_DIR", "/state/requests"),
		ResultsDir:  env("UPDATER_RESULTS_DIR", "/state/results"),
	}
	tg := &bot.HTTPTelegram{
		Base: env("UPDATER_TELEGRAM_API", "https://api.telegram.org"), Token: token,
		Client: &http.Client{Timeout: 70 * time.Second},
	}
	gh := &release.Client{BaseURL: env("UPDATER_GITHUB_API", "https://api.github.com"), Repo: repo, Token: ghToken}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("updater %s started for %s (%d allowed user(s))", version, repo, len(ids))
	if err := bot.New(cfg, tg, gh, nil).Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Print("updater stopped")
}
