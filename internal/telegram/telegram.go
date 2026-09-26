// Package telegram publishes stories to the CFN channel (§29).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

// Kind selects the message header. It mirrors the four types in §29.
type Kind string

const (
	KindBreaking Kind = "breaking"
	KindNormal   Kind = "normal"
	KindSports   Kind = "sports"
	KindVideo    Kind = "video"
)

// headers are the Khmer labels that open each message type.
var headers = map[Kind]string{
	KindBreaking: "🔴 បន្ទាន់",
	KindNormal:   "📰 ព័ត៌មានថ្មី",
	KindSports:   "⚽ កីឡា",
	KindVideo:    "🎥 វីដេអូ",
}

// Message is one post to compose and send.
type Message struct {
	Kind     Kind
	Headline string
	Summary  string
	URL      string
	ImageURL string
}

// Compose renders the message body in Telegram's HTML parse mode.
//
// Every interpolated value is HTML-escaped: a headline containing "<" would
// otherwise make Telegram reject the whole message as malformed markup.
func (m Message) Compose() string {
	header := headers[m.Kind]
	if header == "" {
		header = headers[KindNormal]
	}

	var b strings.Builder
	b.WriteString("<b>" + header + "</b>\n\n")
	b.WriteString("<b>" + html.EscapeString(m.Headline) + "</b>\n")

	if s := strings.TrimSpace(m.Summary); s != "" {
		b.WriteString("\n" + html.EscapeString(utils.Truncate(s, 400)) + "\n")
	}
	if m.URL != "" {
		b.WriteString("\n👉 អានព័ត៌មានពេញ៖\n" + html.EscapeString(m.URL))
	}
	return b.String()
}

// Telegram caps a photo caption at 1024 characters and a text message at 4096.
const (
	maxCaptionLen = 1024
	maxMessageLen = 4096
)

// CredentialsFunc resolves the effective bot token, channel and auto-publish
// flag. It is called per request so a change saved in the admin takes effect
// immediately, without a restart.
type CredentialsFunc func(ctx context.Context) (token, channel string, autoPublish bool)

type Service struct {
	credentials CredentialsFunc
	client      *http.Client
}

// NewService builds a service backed by the environment only. Call
// WithCredentials once the settings service exists to make it dynamic.
func NewService(cfg *config.Config) *Service {
	static := cfg.Telegram
	return &Service{
		credentials: func(context.Context) (string, string, bool) {
			return static.BotToken, static.ChannelID, static.AutoPublish
		},
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

// WithCredentials swaps in a dynamic resolver, so values saved in the admin
// override the environment.
func (s *Service) WithCredentials(fn CredentialsFunc) *Service {
	if fn != nil {
		s.credentials = fn
	}
	return s
}

// Configured reports whether a bot token and channel are present. Callers use
// this to skip silently rather than log an error on every publish.
func (s *Service) Configured(ctx context.Context) bool {
	token, channel, _ := s.credentials(ctx)
	return token != "" && channel != ""
}

// AutoPublishEnabled reports whether publishing an article should post
// automatically (§70). Off by default so a newsroom opts in deliberately.
func (s *Service) AutoPublishEnabled(ctx context.Context) bool {
	_, _, auto := s.credentials(ctx)
	return auto
}

type sendResult struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

// Send posts the message, using sendPhoto when an image is available so the
// channel shows a card rather than a bare link.
func (s *Service) Send(ctx context.Context, m Message) (int64, error) {
	if !s.Configured(ctx) {
		return 0, fmt.Errorf("telegram is not configured")
	}

	body := m.Compose()

	if m.ImageURL != "" {
		// Long posts do not fit in a caption; fall back to a text message so
		// the summary is not silently truncated away.
		if len([]rune(body)) <= maxCaptionLen {
			id, err := s.call(ctx, "sendPhoto", map[string]any{
				"chat_id":    channelOf(ctx, s),
				"photo":      m.ImageURL,
				"caption":    body,
				"parse_mode": "HTML",
			})
			if err == nil {
				return id, nil
			}
			// A rejected photo (unreachable URL, unsupported type) should not
			// cost the newsroom the post — retry as text.
			slog.Warn("telegram sendPhoto failed, retrying as text", "error", err)
		}
	}

	if len([]rune(body)) > maxMessageLen {
		body = utils.Truncate(body, maxMessageLen-1)
	}
	return s.call(ctx, "sendMessage", map[string]any{
		"chat_id":                  channelOf(ctx, s),
		"text":                     body,
		"parse_mode":               "HTML",
		"disable_web_page_preview": false,
	})
}

func (s *Service) call(ctx context.Context, method string, payload map[string]any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode telegram payload: %w", err)
	}

	token, _, _ := s.credentials(ctx)
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/%s", token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call telegram %s: %w", method, err)
	}
	defer resp.Body.Close()

	var result sendResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode telegram response: %w", err)
	}
	if !result.OK {
		// The bot token is in the URL, never in the error we surface or log.
		return 0, fmt.Errorf("telegram %s rejected: %s (code %d)", method, result.Description, result.ErrorCode)
	}
	return result.Result.MessageID, nil
}

// VerifyConnection calls getMe, backing the "Telegram Connected" indicator in
// the admin dashboard (§70).
func (s *Service) VerifyConnection(ctx context.Context) error {
	if !s.Configured(ctx) {
		return fmt.Errorf("telegram is not configured")
	}
	_, err := s.call(ctx, "getMe", map[string]any{})
	return err
}

// VerifyToken checks a candidate token without saving it, so an operator can
// confirm a paste is right before committing it.
func (s *Service) VerifyToken(ctx context.Context, token string) error {
	if token == "" {
		return fmt.Errorf("no token supplied")
	}
	probe := &Service{
		client:      s.client,
		credentials: func(context.Context) (string, string, bool) { return token, "", false },
	}
	_, err := probe.call(ctx, "getMe", map[string]any{})
	return err
}

func channelOf(ctx context.Context, s *Service) string {
	_, channel, _ := s.credentials(ctx)
	return channel
}
