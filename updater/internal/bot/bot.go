// Package bot is the Telegram side of the updater: it long-polls, checks who is asking, checks
// the owner's TOTP code, decides whether a release may be deployed, and hands the request to
// the privileged update unit through a drop-box directory. It never runs anything itself.
package bot

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/realsystem/processor/updater/internal/release"
	"github.com/realsystem/processor/updater/internal/state"
	"github.com/realsystem/processor/updater/internal/totp"
)

// Releases is what the bot needs from GitHub; tests replace it with a fake.
type Releases interface {
	LatestTag(ctx context.Context) (release.Tag, error)
	CI(ctx context.Context, sha string) (release.CIResult, error)
}

// Config holds everything the bot is told from outside.
type Config struct {
	AllowedIDs map[int64]bool
	Repo       string
	RequireCI  bool
	Version    string

	BotDir      string // private state: totp secret, last step, lockout, audit, notify marker
	RequestsDir string // the bot writes update.request here; the host unit consumes it
	ResultsDir  string // the host unit writes update-result.json and current-tag here (read only)

	MaxMessageAge time.Duration
	MaxFailures   int
	FailWindow    time.Duration
	LockFor       time.Duration

	ResultPoll      time.Duration
	ResultStartWait time.Duration
	ResultMaxWait   time.Duration
}

// Defaults fills unset values with the production settings.
func (c *Config) Defaults() {
	if c.MaxMessageAge == 0 {
		c.MaxMessageAge = 2 * time.Minute
	}
	if c.MaxFailures == 0 {
		c.MaxFailures = 5
	}
	if c.FailWindow == 0 {
		c.FailWindow = 15 * time.Minute
	}
	if c.LockFor == 0 {
		c.LockFor = 15 * time.Minute
	}
	if c.ResultPoll == 0 {
		c.ResultPoll = 5 * time.Second
	}
	if c.ResultStartWait == 0 {
		c.ResultStartWait = 2 * time.Minute
	}
	if c.ResultMaxWait == 0 {
		c.ResultMaxWait = 45 * time.Minute
	}
}

// Result is what the host unit writes when an update ends.
type Result struct {
	RequestID  string `json:"request_id"`
	Status     string `json:"status"` // success, failed, rolled_back, refused
	From       string `json:"from"`
	To         string `json:"to"`
	Detail     string `json:"detail"`
	FinishedAt string `json:"finished_at"`
}

type notify struct {
	ChatID      int64  `json:"chat_id"`
	RequestID   string `json:"request_id"`
	Tag         string `json:"tag"`
	From        string `json:"from"`
	RequestedAt int64  `json:"requested_at"`
}

// Bot is the updater bot.
type Bot struct {
	cfg   Config
	tg    Telegram
	rel   Releases
	store *state.Store
	now   func() time.Time
	start time.Time

	mu sync.Mutex // one command at a time: makes "verify, then spend the code" race-free
	wg sync.WaitGroup
}

// New builds a bot. now may be nil (the wall clock).
func New(cfg Config, tg Telegram, rel Releases, now func() time.Time) *Bot {
	cfg.Defaults()
	if now == nil {
		now = time.Now
	}
	return &Bot{
		cfg: cfg, tg: tg, rel: rel, now: now, start: now(),
		store: &state.Store{Dir: cfg.BotDir, Now: now},
	}
}

// Wait blocks until result watchers started by this bot have finished (used by tests).
func (b *Bot) Wait() { b.wg.Wait() }

var codeRegex = regexp.MustCompile(`^[0-9]{6}$`)

// Run long-polls until ctx ends.
func (b *Bot) Run(ctx context.Context) error {
	b.resume(ctx)
	var offset int64
	backoff := time.Second
	for ctx.Err() == nil {
		ups, err := b.tg.GetUpdates(ctx, offset, 50)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("poll failed: %v (retrying in %s)", err, backoff)
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message != nil {
				b.HandleMessage(ctx, *u.Message)
			}
		}
	}
	b.wg.Wait()
	return nil
}

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := b.tg.Send(ctx, chatID, text); err != nil {
		log.Printf("send failed: %v", err)
	}
}

func (b *Bot) audit(userID int64, command, outcome, detail string) {
	if err := b.store.AppendAudit(userID, command, outcome, detail); err != nil {
		log.Printf("audit failed: %v", err)
	}
}

// HandleMessage routes one incoming message.
func (b *Bot) HandleMessage(ctx context.Context, m Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m.From == nil || !strings.HasPrefix(m.Text, "/") {
		return
	}
	fields := strings.Fields(m.Text)
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	if i := strings.Index(cmd, "@"); i >= 0 {
		cmd = cmd[:i]
	}
	args := fields[1:]
	uid := m.From.ID
	allowed := b.cfg.AllowedIDs[uid]

	switch cmd {
	case "id": // anyone may learn their own id; it is how the allowlist gets filled
		b.audit(uid, "/id", "ok", "")
		b.reply(ctx, m.Chat.ID, fmt.Sprintf("Your Telegram user id is %d", uid))
	case "update":
		if !allowed {
			b.audit(uid, "/update", "denied", "not on the allowlist")
			return // ignored: no answer
		}
		b.cmdUpdate(ctx, m, args)
	case "help", "start":
		if allowed {
			b.reply(ctx, m.Chat.ID, helpText)
		}
	case "status":
		if allowed {
			b.reply(ctx, m.Chat.ID, b.statusText())
		}
	default:
		if allowed {
			b.reply(ctx, m.Chat.ID, "Unknown command. /help lists them.")
		}
	}
}

const helpText = `Processor updater
/id - show your Telegram user id
/status - version, uptime, last update result
/update <6-digit code> - deploy the newest release tag (private chat, authenticator code)
/help - this text`

func (b *Bot) readCurrent() string {
	raw, err := os.ReadFile(filepath.Join(b.cfg.ResultsDir, "current-tag"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func (b *Bot) readResult() *Result {
	raw, err := os.ReadFile(filepath.Join(b.cfg.ResultsDir, "update-result.json"))
	if err != nil {
		return nil
	}
	var r Result
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	return &r
}

func (b *Bot) resultFor(id string) *Result {
	if r := b.readResult(); r != nil && r.RequestID == id {
		return r
	}
	return nil
}

func (b *Bot) requestPath() string { return filepath.Join(b.cfg.RequestsDir, "update.request") }
func (b *Bot) notifyPath() string  { return filepath.Join(b.cfg.BotDir, "notify.json") }

func (b *Bot) readNotify() (notify, bool) {
	raw, err := os.ReadFile(b.notifyPath())
	if err != nil {
		return notify{}, false
	}
	var n notify
	if json.Unmarshal(raw, &n) != nil || n.RequestID == "" {
		return notify{}, false
	}
	return n, true
}

func (b *Bot) inProgress() bool {
	if _, err := os.Lstat(b.requestPath()); err == nil {
		return true
	}
	n, ok := b.readNotify()
	if !ok || b.resultFor(n.RequestID) != nil {
		return false
	}
	return b.now().Sub(time.Unix(n.RequestedAt, 0)) < b.cfg.ResultMaxWait
}

func (b *Bot) statusText() string {
	var sb strings.Builder
	cur := b.readCurrent()
	if cur == "" {
		cur = "unknown (no update has run yet)"
	}
	fmt.Fprintf(&sb, "Updater %s, up %s\nRepo: %s (require green CI: %v)\nDeployed: %s\n",
		b.cfg.Version, b.now().Sub(b.start).Round(time.Second), b.cfg.Repo, b.cfg.RequireCI, cur)
	if r := b.readResult(); r != nil {
		fmt.Fprintf(&sb, "Last update: %s (%s -> %s) at %s", r.Status, r.From, r.To, r.FinishedAt)
		if r.Detail != "" {
			fmt.Fprintf(&sb, "\n  %s", r.Detail)
		}
	} else {
		sb.WriteString("Last update: none")
	}
	if b.inProgress() {
		sb.WriteString("\nAn update is in progress.")
	}
	return sb.String()
}

// refuse answers, audits and ends a command that did not go through.
func (b *Bot) refuse(ctx context.Context, m Message, outcome, userText, detail string) {
	b.audit(m.From.ID, "/update", outcome, detail)
	b.reply(ctx, m.Chat.ID, userText)
}

func (b *Bot) readSecret() ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(b.cfg.BotDir, "totp.secret"))
	if err != nil {
		return nil, err
	}
	return totp.DecodeSecret(string(raw))
}

func (b *Bot) cmdUpdate(ctx context.Context, m Message, args []string) {
	uid := m.From.ID
	// 1. private chat only: a code typed in a group is visible to the whole group
	if m.Chat.Type != "private" {
		b.refuse(ctx, m, "refused_not_private",
			"Updates are only accepted in a private chat with me. Do not post codes in groups.", "chat type "+m.Chat.Type)
		return
	}
	// 2. fresh message: an old, replayed or queued message must not act
	if age := b.now().Sub(time.Unix(m.Date, 0)); age > b.cfg.MaxMessageAge {
		b.refuse(ctx, m, "refused_expired", "That message is too old. Send /update with a fresh code.", "message older than limit")
		return
	}
	// 3. exactly one argument of exactly six digits
	if len(args) != 1 || !codeRegex.MatchString(args[0]) {
		b.refuse(ctx, m, "refused_bad_format", "Usage: /update 123456 (the 6-digit code from your authenticator app)", "bad format")
		return
	}
	code := args[0]
	_ = b.tg.Delete(ctx, m.Chat.ID, m.MessageID) // keep the code out of the chat history
	// 4. lockout
	if until, locked := b.store.LockedUntil(); locked {
		b.refuse(ctx, m, "refused_locked",
			fmt.Sprintf("Too many wrong codes. Locked for another %s.", until.Sub(b.now()).Round(time.Minute)), "locked out")
		return
	}
	// 5. refuse a second update before a code is spent on it
	if b.inProgress() {
		b.refuse(ctx, m, "refused_in_progress", "An update is already in progress. I will message you when it is done.", "in progress")
		return
	}
	// 6. the owner's code
	key, err := b.readSecret()
	if err != nil {
		log.Printf("totp secret unavailable: %v", err)
		b.refuse(ctx, m, "refused_no_totp", "TOTP is not set up on this host. Run the updater with --totp-setup first.", "no secret")
		return
	}
	step, ok := totp.Match(key, code, b.now())
	last, lerr := b.store.LastStep()
	if lerr != nil {
		log.Printf("read last step: %v", lerr)
		b.refuse(ctx, m, "refused_state_error", "Cannot read my state; refusing.", "state error")
		return
	}
	if !ok || step <= last {
		outcome, text := "refused_wrong_code", "Wrong code."
		if ok {
			outcome, text = "refused_reused_code", "That code was already used. Wait for the next one."
		}
		locked, ferr := b.store.RecordFailure(b.cfg.MaxFailures, b.cfg.FailWindow, b.cfg.LockFor)
		if ferr != nil {
			log.Printf("record failure: %v", ferr)
		}
		if locked {
			text += fmt.Sprintf(" Too many failures: locked for %s.", b.cfg.LockFor)
			outcome += "_locked"
		}
		b.refuse(ctx, m, outcome, text, "")
		return
	}
	_ = b.store.ClearFailures()

	// 7. what to deploy: only the newest release tag
	tag, err := b.rel.LatestTag(ctx)
	if err != nil {
		var rl *release.RateLimitError
		switch {
		case errors.Is(err, release.ErrNoTag):
			b.refuse(ctx, m, "refused_no_tag", "No release tag (vMAJOR.MINOR[.PATCH]) exists in "+b.cfg.Repo+". Cut a tag first.", "no tag")
		case errors.As(err, &rl):
			b.refuse(ctx, m, "refused_rate_limited", "GitHub's API rate limit is used up: "+rl.Error()+". Try later.", "rate limited")
		default:
			log.Printf("latest tag: %v", err)
			b.refuse(ctx, m, "refused_github_error", "Could not ask GitHub for the newest release. Try again later.", "github error")
		}
		return
	}
	current := b.readCurrent()
	if current == tag.Name {
		b.refuse(ctx, m, "already_current", "Already on the newest release, "+tag.Name+". Nothing to do.", "")
		return
	}
	latest, _ := release.Parse(tag.Name)
	if cur, isRelease := release.Parse(current); isRelease && cur.Newer(latest) {
		b.refuse(ctx, m, "refused_downgrade",
			fmt.Sprintf("Deployed %s is newer than the newest release tag %s. Not downgrading.", current, tag.Name), "downgrade")
		return
	}
	// 8. green CI on the tagged commit
	if b.cfg.RequireCI {
		ci, err := b.rel.CI(ctx, tag.SHA)
		if err != nil {
			var rl *release.RateLimitError
			if errors.As(err, &rl) {
				b.refuse(ctx, m, "refused_rate_limited", "GitHub's API rate limit is used up: "+rl.Error()+". Try later.", "rate limited")
				return
			}
			log.Printf("ci check: %v", err)
			b.refuse(ctx, m, "refused_ci_error", "Could not read CI results for "+tag.Name+" from GitHub. Refusing to deploy unchecked code.", "ci api error")
			return
		}
		switch ci.State {
		case release.CINone:
			b.refuse(ctx, m, "refused_ci_none", tag.Name+" has no CI runs, so it cannot be verified. Refusing.", ci.Detail)
			return
		case release.CIPending:
			b.refuse(ctx, m, "refused_ci_pending", "CI is still running on "+tag.Name+". Try again when it finishes. ("+ci.Detail+")", ci.Detail)
			return
		case release.CIFailed:
			b.refuse(ctx, m, "refused_ci_failed", "CI failed on "+tag.Name+". Refusing. ("+ci.Detail+")", ci.Detail)
			return
		}
	}
	// 9. spend the code, then drop the request
	if err := b.store.SetLastStep(step); err != nil {
		log.Printf("persist step: %v", err)
		b.refuse(ctx, m, "refused_state_error", "Cannot record the code as used; refusing.", "state error")
		return
	}
	id, err := newRequestID(b.now())
	if err != nil {
		b.refuse(ctx, m, "failed_request", "Could not create the request.", "id")
		return
	}
	if err := b.dropRequest(tag.Name, id); err != nil {
		if errors.Is(err, os.ErrExist) {
			b.refuse(ctx, m, "refused_in_progress", "An update is already in progress. I will message you when it is done.", "request exists")
			return
		}
		log.Printf("write request: %v", err)
		b.refuse(ctx, m, "failed_request", "Could not hand the request to the host: check the requests directory.", "write failed")
		return
	}
	n := notify{ChatID: m.Chat.ID, RequestID: id, Tag: tag.Name, From: current, RequestedAt: b.now().Unix()}
	nb, _ := json.Marshal(n)
	if err := state.WriteFileAtomic(b.notifyPath(), nb, 0o600); err != nil {
		log.Printf("write notify marker: %v", err)
	}
	b.audit(uid, "/update", "requested", "to "+tag.Name)
	from := current
	if from == "" {
		from = "unknown version"
	}
	b.reply(ctx, m.Chat.ID, fmt.Sprintf("Code accepted. Updating %s -> %s. This can take a few minutes; I will message you with the result, even if I restart.", from, tag.Name))
	b.watch(ctx, n)
}

func newRequestID(now time.Time) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return fmt.Sprintf("%x%x", now.Unix(), buf), nil
}

// dropRequest creates update.request atomically: the file appears complete or not at all, and
// creating it fails if one is already there (a second request is refused, not queued).
func (b *Bot) dropRequest(tag, id string) error {
	tmp, err := os.CreateTemp(b.cfg.RequestsDir, ".req-*")
	if err != nil {
		return fmt.Errorf("create temp request: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.WriteString(tag + "\n" + id + "\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write request: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod request: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close request: %w", err)
	}
	if err := os.Link(name, b.requestPath()); err != nil {
		return fmt.Errorf("publish request: %w", err)
	}
	return nil
}

func formatResult(r Result) string {
	switch r.Status {
	case "success":
		return fmt.Sprintf("Update finished: %s -> %s. %s", r.From, r.To, r.Detail)
	case "rolled_back":
		return fmt.Sprintf("Update to %s failed its health check and was rolled back to %s. %s", r.To, r.From, r.Detail)
	case "refused":
		return fmt.Sprintf("Update refused by the host: %s", r.Detail)
	default:
		return fmt.Sprintf("Update to %s failed. %s", r.To, r.Detail)
	}
}

// watch waits for the host's result in the background and reports it, then clears the marker.
func (b *Bot) watch(ctx context.Context, n notify) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		warned := false
		for {
			if r := b.resultFor(n.RequestID); r != nil {
				b.reply(ctx, n.ChatID, formatResult(*r))
				_ = os.Remove(b.notifyPath())
				return
			}
			waited := b.now().Sub(time.Unix(n.RequestedAt, 0))
			if !warned && waited > b.cfg.ResultStartWait {
				if _, err := os.Lstat(b.requestPath()); err == nil {
					b.reply(ctx, n.ChatID, "The host has not picked up the request yet. Is processor-update.path active?")
					warned = true
				}
			}
			if waited > b.cfg.ResultMaxWait {
				b.reply(ctx, n.ChatID, "No result after "+b.cfg.ResultMaxWait.String()+". Check the host: systemctl status processor-update.service")
				_ = os.Remove(b.notifyPath())
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(b.cfg.ResultPoll):
			}
		}
	}()
}

// resume runs at start-up: an update asked for before a restart (or one that replaced this
// very bot) still gets its answer.
func (b *Bot) resume(ctx context.Context) {
	n, ok := b.readNotify()
	if !ok {
		return
	}
	log.Printf("resuming the wait for update request %s", n.RequestID)
	b.watch(ctx, n)
}
