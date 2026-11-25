package telegrambot

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type sent struct {
	chat int64
	text string
	kb   [][]Button
}

type fakeTG struct {
	mu        sync.Mutex
	msgs      []sent
	callbacks []string
}

func (f *fakeTG) GetUpdates(context.Context, int64, int) ([]Update, error) { return nil, nil }
func (f *fakeTG) SendMessage(_ context.Context, chat int64, text string, kb [][]Button) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, sent{chat, text, kb})
	return nil
}
func (f *fakeTG) AnswerCallback(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callbacks = append(f.callbacks, text)
	return nil
}
func (f *fakeTG) SendChatAction(context.Context, int64, string) error { return nil }

func (f *fakeTG) last() sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.msgs) == 0 {
		return sent{}
	}
	return f.msgs[len(f.msgs)-1]
}

func (f *fakeTG) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, m := range f.msgs {
		b.WriteString(m.text + "\n")
	}
	return b.String()
}

type fakeBE struct {
	mu        sync.Mutex
	submitted []map[string]interface{}
	sensitive []bool
	job       Job
	submitErr error
	block     chan struct{} // when set, Wait blocks until it is closed
}

func (f *fakeBE) Models(context.Context) ([]string, string, error) {
	return []string{"llama3.2", "qwen2.5:7b", "gemma2:2b"}, "llama3.2", nil
}
func (f *fakeBE) Submit(_ context.Context, in map[string]interface{}, sens bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.submitErr != nil {
		return "", f.submitErr
	}
	f.submitted = append(f.submitted, in)
	f.sensitive = append(f.sensitive, sens)
	return "job_1", nil
}
func (f *fakeBE) Wait(ctx context.Context, _ string) (Job, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return Job{}, ctx.Err()
		}
	}
	return f.job, nil
}

func okJob(content string) Job {
	j := Job{ID: "job_1", Status: "succeeded"}
	j.Result.Content, j.Result.Model, j.Result.DurationSeconds = content, "llama3.2", 1.5
	return j
}

func newTestBot(be *fakeBE) (*Bot, *fakeTG) {
	tg := &fakeTG{}
	b := New(tg, be, Config{Allowed: map[int64]bool{42: true}, Sensitive: true, JobTimeout: 5 * time.Second})
	_ = b.refreshModels(context.Background())
	return b, tg
}

func say(b *Bot, user int64, text string) {
	b.Handle(context.Background(), Update{Message: &Message{From: &User{ID: user}, Chat: Chat{ID: user}, Text: text}})
}

func TestUnauthorizedUserCannotSubmit(t *testing.T) {
	be := &fakeBE{job: okJob("x")}
	b, tg := newTestBot(be)
	say(b, 7, "hello")
	b.Wait()
	if len(be.submitted) != 0 {
		t.Fatal("a user outside the allowlist started a job")
	}
	if !strings.Contains(tg.last().text, "7") {
		t.Errorf("the refusal should tell them their user id: %q", tg.last().text)
	}
}

func TestPlainTextRunsOnTheDefaultModel(t *testing.T) {
	be := &fakeBE{job: okJob("Paris")}
	b, tg := newTestBot(be)
	say(b, 42, "capital of France?")
	b.Wait()
	if len(be.submitted) != 1 || be.submitted[0]["prompt"] != "capital of France?" || be.submitted[0]["model"] != "llama3.2" {
		t.Fatalf("submitted %v", be.submitted)
	}
	if !be.sensitive[0] {
		t.Error("jobs must be marked sensitive by default")
	}
	if got := tg.last().text; !strings.HasPrefix(got, "Paris") || !strings.Contains(got, "llama3.2") {
		t.Errorf("reply %q", got)
	}
}

func TestModelChoiceSticksAndOverrideDoesNot(t *testing.T) {
	be := &fakeBE{job: okJob("ok")}
	b, tg := newTestBot(be)

	say(b, 42, "/model QWEN2.5:7b") // case-insensitive
	if !strings.Contains(tg.last().text, "qwen2.5:7b") {
		t.Fatalf("reply %q", tg.last().text)
	}
	say(b, 42, "first")
	b.Wait()
	say(b, 42, "@gemma2:2b second") // one-shot override
	b.Wait()
	say(b, 42, "/ask third")
	b.Wait()

	var models []interface{}
	for _, in := range be.submitted {
		models = append(models, in["model"])
	}
	want := []interface{}{"qwen2.5:7b", "gemma2:2b", "qwen2.5:7b"}
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("models used %v, want %v", models, want)
		}
	}
	if be.submitted[1]["prompt"] != "second" {
		t.Errorf("the @model prefix must not be part of the prompt: %v", be.submitted[1])
	}
}

func TestUnknownModelIsRefused(t *testing.T) {
	be := &fakeBE{job: okJob("ok")}
	b, tg := newTestBot(be)
	say(b, 42, "/model nope:1b")
	if !strings.Contains(tg.last().text, "unknown model") || !strings.Contains(tg.last().text, "gemma2:2b") {
		t.Errorf("should say what is available: %q", tg.last().text)
	}
	say(b, 42, "@nope:1b hi")
	b.Wait()
	if len(be.submitted) != 0 {
		t.Error("a question with an unknown model must not be submitted")
	}
}

func TestModelsKeyboardMarksTheCurrentModel(t *testing.T) {
	b, tg := newTestBot(&fakeBE{})
	say(b, 42, "/models")
	kb := tg.last().kb
	if len(kb) != 3 {
		t.Fatalf("keyboard %v", kb)
	}
	if kb[0][0].Text != "✓ llama3.2" || kb[1][0].CallbackData != "model:qwen2.5:7b" {
		t.Errorf("keyboard %v", kb)
	}
}

func TestCallbackSetsTheModel(t *testing.T) {
	be := &fakeBE{job: okJob("ok")}
	b, tg := newTestBot(be)
	cb := func(user int64) Update {
		return Update{CallbackQuery: &CallbackQuery{ID: "c", From: User{ID: user}, Data: "model:gemma2:2b", Message: &Message{Chat: Chat{ID: 42}}}}
	}
	b.Handle(context.Background(), cb(7)) // not allowed
	say(b, 42, "q")
	b.Wait()
	if be.submitted[0]["model"] != "llama3.2" {
		t.Fatal("a callback from an unauthorized user changed the model")
	}
	b.Handle(context.Background(), cb(42))
	say(b, 42, "q2")
	b.Wait()
	if be.submitted[1]["model"] != "gemma2:2b" {
		t.Fatalf("submitted %v", be.submitted)
	}
	if len(tg.callbacks) != 2 || tg.callbacks[0] != "Not authorized" {
		t.Errorf("callbacks %v", tg.callbacks)
	}
}

func TestFailedJobIsReported(t *testing.T) {
	be := &fakeBE{job: Job{ID: "job_1", Status: "dead_letter", ErrorText: "LLM returned status 404: model not found"}}
	b, tg := newTestBot(be)
	say(b, 42, "hi")
	b.Wait()
	if got := tg.last().text; !strings.Contains(got, "dead_letter") || !strings.Contains(got, "model not found") {
		t.Errorf("reply %q", got)
	}
}

func TestSubmitErrorIsReported(t *testing.T) {
	b, tg := newTestBot(&fakeBE{submitErr: errors.New("control plane down")})
	say(b, 42, "hi")
	b.Wait()
	if !strings.Contains(tg.last().text, "control plane down") {
		t.Errorf("reply %q", tg.last().text)
	}
}

func TestInFlightLimitPerChat(t *testing.T) {
	be := &fakeBE{job: okJob("ok"), block: make(chan struct{})}
	b, tg := newTestBot(be)
	for i := 0; i < 5; i++ {
		say(b, 42, "q")
	}
	if !strings.Contains(tg.all(), "Too many unanswered") {
		t.Error("the fourth and fifth question should have been refused")
	}
	close(be.block)
	b.Wait()
	if len(be.submitted) != 3 {
		t.Errorf("submitted %d jobs, want 3", len(be.submitted))
	}
}

func TestEmptyAskAndUnknownCommand(t *testing.T) {
	be := &fakeBE{job: okJob("ok")}
	b, tg := newTestBot(be)
	say(b, 42, "/ask")
	say(b, 42, "/ask@MyBot")
	say(b, 42, "/nope")
	b.Wait()
	if len(be.submitted) != 0 {
		t.Fatal("nothing should be submitted")
	}
	if !strings.Contains(tg.last().text, "Unknown command") {
		t.Errorf("reply %q", tg.last().text)
	}
}

func TestRunRefusesToRunWithoutAllowedUsers(t *testing.T) {
	b := New(&fakeTG{}, &fakeBE{}, Config{})
	if err := b.Run(context.Background()); err == nil {
		t.Fatal("an open bot must not start")
	}
}

func TestSplitMessage(t *testing.T) {
	long := strings.Repeat("word ", 2000) // 10000 runes
	parts := splitMessage(long, 4000)
	if len(parts) != 3 {
		t.Fatalf("%d parts", len(parts))
	}
	for _, p := range parts {
		if len([]rune(p)) > 4000 {
			t.Errorf("part of %d runes", len([]rune(p)))
		}
	}
	if got := strings.Join(parts, " "); strings.Count(got, "word") != 2000 {
		t.Error("text was lost or duplicated while splitting")
	}
	if got := splitMessage("short", 4000); len(got) != 1 || got[0] != "short" {
		t.Errorf("%v", got)
	}
	uni := strings.Repeat("é", 5000)
	for _, p := range splitMessage(uni, 4000) {
		if !strings.HasPrefix(p, "é") {
			t.Error("a multi-byte character was cut in half")
		}
	}
}

func TestParseAllowedUsers(t *testing.T) {
	got, err := ParseAllowedUsers(" 1, 22 ,,333")
	if err != nil || !got[1] || !got[22] || !got[333] || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := ParseAllowedUsers("12,abc"); err == nil {
		t.Error("a non-numeric id must be an error")
	}
}
