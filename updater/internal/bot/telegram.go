package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Update, Message, Chat and User are the few Telegram fields the bot reads.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message is an incoming chat message.
type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
}

// Chat identifies where a message came from.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// User is the sender.
type User struct {
	ID int64 `json:"id"`
}

// Telegram is what the bot needs from the Bot API; tests replace it with a fake.
type Telegram interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
	Send(ctx context.Context, chatID int64, text string) error
	Delete(ctx context.Context, chatID int64, messageID int) error
}

// HTTPTelegram calls the real Bot API over outbound HTTPS (long polling, no webhook).
type HTTPTelegram struct {
	Base   string // https://api.telegram.org
	Token  string
	Client *http.Client
}

type apiReply struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

// scrub removes the request URL, which contains the bot token, from transport errors.
func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func (t *HTTPTelegram) call(ctx context.Context, method string, params url.Values, out any) error {
	endpoint := strings.TrimRight(t.Base, "/") + "/bot" + t.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("telegram %s: build request failed", method)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 70 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, scrub(err))
	}
	defer resp.Body.Close()
	var r apiReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: status %d, unreadable answer", method, resp.StatusCode)
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %s (code %d)", method, r.Description, r.ErrorCode)
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

// GetUpdates long-polls for new messages.
func (t *HTTPTelegram) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	p := url.Values{}
	p.Set("offset", strconv.FormatInt(offset, 10))
	p.Set("timeout", strconv.Itoa(timeoutSec))
	p.Set("allowed_updates", `["message"]`)
	var ups []Update
	if err := t.call(ctx, "getUpdates", p, &ups); err != nil {
		return nil, err
	}
	return ups, nil
}

// Send posts plain text (no parse mode, so nothing in it is interpreted).
func (t *HTTPTelegram) Send(ctx context.Context, chatID int64, text string) error {
	p := url.Values{}
	p.Set("chat_id", strconv.FormatInt(chatID, 10))
	p.Set("text", text)
	return t.call(ctx, "sendMessage", p, nil)
}

// Delete removes a message (used for the one carrying the code).
func (t *HTTPTelegram) Delete(ctx context.Context, chatID int64, messageID int) error {
	p := url.Values{}
	p.Set("chat_id", strconv.FormatInt(chatID, 10))
	p.Set("message_id", strconv.Itoa(messageID))
	return t.call(ctx, "deleteMessage", p, nil)
}
