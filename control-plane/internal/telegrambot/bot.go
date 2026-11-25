package telegrambot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Config holds the bot's settings.
type Config struct {
	// Allowed is the set of Telegram user ids that may use the bot. It must not be empty:
	// every message is a GPU job, so an open bot would let anyone who finds it spend yours.
	Allowed map[int64]bool
	// DefaultModel is used until a chat picks one; empty means the control plane's default.
	DefaultModel string
	// JobTimeout bounds how long the bot waits for one answer (the job itself keeps running).
	JobTimeout time.Duration
	// Sensitive marks the jobs so the control plane keeps prompts and answers out of webhooks
	// and e-mails and erases them after its retention period.
	Sensitive bool
	// MaxInFlight caps unfinished questions per chat (default 3).
	MaxInFlight int
}

// Bot answers Telegram messages by submitting llm.chat jobs to the control plane.
type Bot struct {
	tg  Messenger
	be  Backend
	cfg Config

	mu       sync.Mutex
	models   []string
	def      string
	chosen   map[int64]string // chat id -> model picked with /model
	inflight map[int64]int
	wg       sync.WaitGroup
}

func New(tg Messenger, be Backend, cfg Config) *Bot {
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 15 * time.Minute
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 3
	}
	return &Bot{tg: tg, be: be, cfg: cfg, chosen: map[int64]string{}, inflight: map[int64]int{}}
}

// ParseAllowedUsers parses a comma-separated list of Telegram user ids.
func ParseAllowedUsers(s string) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		id, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a Telegram user id (a number)", f)
		}
		out[id] = true
	}
	return out, nil
}

// Run long-polls Telegram until ctx ends, then waits for the running questions.
func (b *Bot) Run(ctx context.Context) error {
	if len(b.cfg.Allowed) == 0 {
		return fmt.Errorf("no allowed Telegram user ids: refusing to run an open bot")
	}
	if err := b.refreshModels(ctx); err != nil {
		log.Printf("telegram bot: cannot read the model list yet: %v", err)
	}
	var offset int64
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset, 30)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			log.Printf("telegram bot: getUpdates: %v", err)
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.Handle(ctx, u)
		}
	}
	b.wg.Wait()
	return nil
}

// Wait blocks until every question started so far has been answered (used by tests).
func (b *Bot) Wait() { b.wg.Wait() }

// Handle processes one update. Questions run in the background; Handle returns quickly.
func (b *Bot) Handle(ctx context.Context, u Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	}
}

func (b *Bot) allowed(userID int64) bool { return b.cfg.Allowed[userID] }

func (b *Bot) reply(ctx context.Context, chatID int64, text string) {
	for _, part := range splitMessage(text, 4000) {
		if err := b.tg.SendMessage(ctx, chatID, part, nil); err != nil {
			log.Printf("telegram bot: sendMessage to chat %d: %v", chatID, err)
			return
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, m *Message) {
	if m.From == nil {
		return
	}
	if !b.allowed(m.From.ID) {
		log.Printf("telegram bot: refused user %d", m.From.ID)
		b.reply(ctx, m.Chat.ID, fmt.Sprintf("Not authorized. Your Telegram user id is %d: ask the owner to add it to TELEGRAM_ALLOWED_USERS.", m.From.ID))
		return
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		return
	}
	if !strings.HasPrefix(text, "/") {
		b.ask(ctx, m.Chat.ID, text)
		return
	}
	cmd, arg := text, ""
	if i := strings.IndexAny(text, " \n\t"); i >= 0 {
		cmd, arg = text[:i], strings.TrimSpace(text[i+1:])
	}
	if i := strings.Index(cmd, "@"); i >= 0 { // /ask@MyBot
		cmd = cmd[:i]
	}
	switch strings.ToLower(cmd) {
	case "/start", "/help":
		b.reply(ctx, m.Chat.ID, helpText)
	case "/models":
		b.showModels(ctx, m.Chat.ID)
	case "/model":
		if arg == "" {
			b.showModels(ctx, m.Chat.ID)
		} else {
			b.setModel(ctx, m.Chat.ID, arg)
		}
	case "/ask":
		b.ask(ctx, m.Chat.ID, arg)
	default:
		b.reply(ctx, m.Chat.ID, "Unknown command. Send /help.")
	}
}

const helpText = `Send any message and I'll answer it with an LLM running on the worker.

/models - choose the model (buttons)
/model <name> - choose the model by name
/ask <question> - same as sending the question as plain text
@<model> <question> - use another model for this one question

Bigger models give better answers but are slower; a long answer can take minutes.`

func (b *Bot) handleCallback(ctx context.Context, cb *CallbackQuery) {
	if !b.allowed(cb.From.ID) {
		_ = b.tg.AnswerCallback(ctx, cb.ID, "Not authorized")
		return
	}
	name, ok := strings.CutPrefix(cb.Data, "model:")
	if !ok || cb.Message == nil {
		_ = b.tg.AnswerCallback(ctx, cb.ID, "")
		return
	}
	model, err := b.resolveModel(ctx, name)
	if err != nil {
		_ = b.tg.AnswerCallback(ctx, cb.ID, err.Error())
		return
	}
	b.mu.Lock()
	b.chosen[cb.Message.Chat.ID] = model
	b.mu.Unlock()
	_ = b.tg.AnswerCallback(ctx, cb.ID, "Model: "+model)
	b.reply(ctx, cb.Message.Chat.ID, "Model set to "+model)
}

// refreshModels reloads the list from the control plane, keeping the old one on failure.
func (b *Bot) refreshModels(ctx context.Context) error {
	models, def, err := b.be.Models(ctx)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.models, b.def = models, def
	b.mu.Unlock()
	return nil
}

// currentModel is the chat's pick, else the configured default, else the control plane's.
func (b *Bot) currentModel(chatID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if m := b.chosen[chatID]; m != "" {
		return m
	}
	if b.cfg.DefaultModel != "" {
		return b.cfg.DefaultModel
	}
	return b.def
}

// resolveModel matches name (case-insensitively) against the control plane's model list.
func (b *Bot) resolveModel(ctx context.Context, name string) (string, error) {
	find := func() string {
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, m := range b.models {
			if strings.EqualFold(m, name) {
				return m
			}
		}
		return ""
	}
	if m := find(); m != "" {
		return m, nil
	}
	_ = b.refreshModels(ctx) // the list may have changed since startup
	if m := find(); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("unknown model %q", name)
}

func (b *Bot) showModels(ctx context.Context, chatID int64) {
	if err := b.refreshModels(ctx); err != nil {
		log.Printf("telegram bot: models: %v", err)
	}
	b.mu.Lock()
	models := append([]string(nil), b.models...)
	b.mu.Unlock()
	if len(models) == 0 {
		b.reply(ctx, chatID, "I can't read the model list from the control plane right now.")
		return
	}
	cur := b.currentModel(chatID)
	var kb [][]Button
	for _, m := range models {
		data := "model:" + m
		if len(data) > 64 { // Telegram's callback_data limit; /model <name> still works
			continue
		}
		label := m
		if m == cur {
			label = "✓ " + m
		}
		kb = append(kb, []Button{{Text: label, CallbackData: data}})
	}
	if err := b.tg.SendMessage(ctx, chatID, "Current model: "+cur+"\nChoose a model:", kb); err != nil {
		log.Printf("telegram bot: sendMessage to chat %d: %v", chatID, err)
	}
}

func (b *Bot) setModel(ctx context.Context, chatID int64, name string) {
	model, err := b.resolveModel(ctx, name)
	if err != nil {
		b.mu.Lock()
		avail := strings.Join(b.models, ", ")
		b.mu.Unlock()
		b.reply(ctx, chatID, err.Error()+". Available: "+avail)
		return
	}
	b.mu.Lock()
	b.chosen[chatID] = model
	b.mu.Unlock()
	b.reply(ctx, chatID, "Model set to "+model)
}

// ask submits one question. A leading "@model " overrides the chat's model for this question.
func (b *Bot) ask(ctx context.Context, chatID int64, text string) {
	text = strings.TrimSpace(text)
	model := ""
	if strings.HasPrefix(text, "@") {
		first, rest := text, ""
		if i := strings.IndexAny(text, " \n\t"); i >= 0 {
			first, rest = text[:i], strings.TrimSpace(text[i+1:])
		}
		m, err := b.resolveModel(ctx, strings.TrimPrefix(first, "@"))
		if err != nil {
			b.reply(ctx, chatID, err.Error()+". Send /models to see the list.")
			return
		}
		model, text = m, rest
	}
	if text == "" {
		b.reply(ctx, chatID, "Send a question, for example: /ask why is the sky blue?")
		return
	}
	if model == "" {
		model = b.currentModel(chatID)
	}

	b.mu.Lock()
	if b.inflight[chatID] >= b.cfg.MaxInFlight {
		b.mu.Unlock()
		b.reply(ctx, chatID, "Too many unanswered questions in this chat, wait for one to finish.")
		return
	}
	b.inflight[chatID]++
	b.mu.Unlock()

	label := model
	if label == "" {
		label = "the default model"
	}
	b.reply(ctx, chatID, "Asking "+label+"... (the worker runs one job at a time)")

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer func() {
			b.mu.Lock()
			b.inflight[chatID]--
			b.mu.Unlock()
		}()
		b.runJob(ctx, chatID, model, text)
	}()
}

func (b *Bot) runJob(ctx context.Context, chatID int64, model, prompt string) {
	input := map[string]interface{}{"prompt": prompt}
	if model != "" {
		input["model"] = model
	}
	id, err := b.be.Submit(ctx, input, b.cfg.Sensitive)
	if err != nil {
		log.Printf("telegram bot: submit for chat %d: %v", chatID, err)
		b.reply(ctx, chatID, "Could not submit the job: "+truncate(err.Error(), 300))
		return
	}
	log.Printf("telegram bot: chat %d job %s model %q submitted", chatID, id, model) // never the prompt

	waitCtx, cancel := context.WithTimeout(ctx, b.cfg.JobTimeout)
	defer cancel()
	stop := b.typing(waitCtx, chatID)
	defer stop()

	job, err := b.be.Wait(waitCtx, id)
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down
		}
		b.reply(ctx, chatID, fmt.Sprintf("Still no answer after %s. Job %s stays in the queue; I won't wait for it any longer.", b.cfg.JobTimeout, id))
		return
	}
	log.Printf("telegram bot: chat %d job %s finished: %s", chatID, id, job.Status)
	if job.Status != "succeeded" {
		b.reply(ctx, chatID, fmt.Sprintf("The job %s (%s): %s", id, job.Status, truncate(job.ErrorText, 300)))
		return
	}
	answer := strings.TrimSpace(job.Result.Content)
	if answer == "" {
		answer = "(empty answer)"
	}
	foot := ""
	if job.Result.Model != "" {
		foot = fmt.Sprintf("\n\n- %s, %.1fs", job.Result.Model, job.Result.DurationSeconds)
	}
	b.reply(ctx, chatID, answer+foot)
}

// typing shows "typing..." in the chat until the returned stop function is called.
func (b *Bot) typing(ctx context.Context, chatID int64) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(4 * time.Second)
		defer t.Stop()
		for {
			_ = b.tg.SendChatAction(ctx, chatID, "typing")
			select {
			case <-t.C:
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() { close(done) }
}

// splitMessage cuts s into pieces of at most limit runes (Telegram allows 4096), preferring to
// break at a newline or space.
func splitMessage(s string, limit int) []string {
	r := []rune(s)
	var out []string
	for len(r) > limit {
		cut := limit
		for i := limit; i > limit/2; i-- {
			if r[i-1] == '\n' || r[i-1] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimRight(string(r[:cut]), " \n"))
		r = r[cut:]
	}
	if len(out) == 0 || strings.TrimSpace(string(r)) != "" {
		out = append(out, string(r))
	}
	return out
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}
