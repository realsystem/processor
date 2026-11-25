// Package telegrambot is a Telegram front end for the Processor control plane: people chat
// with a bot, it submits llm.chat jobs through the control plane's public API and sends the
// answers back. It needs no database and no access to the workers.
package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Update is one event from Telegram's getUpdates.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID int64 `json:"id"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// Button is one inline-keyboard button.
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// Messenger is the part of the Telegram Bot API the bot uses.
type Messenger interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error)
	SendMessage(ctx context.Context, chatID int64, text string, keyboard [][]Button) error
	AnswerCallback(ctx context.Context, id, text string) error
	SendChatAction(ctx context.Context, chatID int64, action string) error
}

// Telegram talks to the Telegram Bot API over HTTPS.
type Telegram struct {
	baseURL string // https://api.telegram.org, or a test server
	token   string
	client  *http.Client
}

func NewTelegram(baseURL, token string) *Telegram {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &Telegram{baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Timeout: 60 * time.Second}}
}

// call posts a JSON request and decodes result. Errors never contain the bot token, which
// is part of the request URL and would otherwise show up in net/http's error text.
func (t *Telegram) call(ctx context.Context, method string, params, result interface{}) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/bot"+t.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return t.redact(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return t.redact(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return t.redact(err)
	}
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("telegram %s: status %d, unreadable reply", method, resp.StatusCode)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s (status %d)", method, env.Description, resp.StatusCode)
	}
	if result != nil {
		return json.Unmarshal(env.Result, result)
	}
	return nil
}

func (t *Telegram) redact(err error) error {
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), t.token, "<token>"))
}

func (t *Telegram) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	var out []Update
	err := t.call(ctx, "getUpdates", map[string]interface{}{
		"offset": offset, "timeout": timeoutSec, "allowed_updates": []string{"message", "callback_query"},
	}, &out)
	return out, err
}

func (t *Telegram) SendMessage(ctx context.Context, chatID int64, text string, keyboard [][]Button) error {
	p := map[string]interface{}{"chat_id": chatID, "text": text}
	if len(keyboard) > 0 {
		p["reply_markup"] = map[string]interface{}{"inline_keyboard": keyboard}
	}
	return t.call(ctx, "sendMessage", p, nil)
}

func (t *Telegram) AnswerCallback(ctx context.Context, id, text string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]interface{}{"callback_query_id": id, "text": text}, nil)
}

func (t *Telegram) SendChatAction(ctx context.Context, chatID int64, action string) error {
	return t.call(ctx, "sendChatAction", map[string]interface{}{"chat_id": chatID, "action": action}, nil)
}
