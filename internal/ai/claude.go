// Package ai wraps the Anthropic API for the newsroom assistants (§21–§24).
//
// Every function here returns a draft. Nothing in this package writes to the
// articles table, changes a status, or triggers distribution — publishing is
// exclusively a human action taken through the editorial endpoints.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/cambodia-fast-news/backend/internal/config"
	"github.com/cambodia-fast-news/backend/internal/utils"
)

var ErrNotConfigured = errors.New("ai assistant is not configured")

type Service struct {
	client  anthropic.Client
	model   string
	enabled bool
}

func NewService(cfg *config.Config) *Service {
	s := &Service{model: cfg.AI.Model, enabled: cfg.AI.Configured()}
	if s.enabled {
		s.client = anthropic.NewClient(option.WithAPIKey(cfg.AI.APIKey))
	}
	return s
}

func (s *Service) Configured() bool { return s != nil && s.enabled }

// BreakingDraftInput is what the journalist supplies. Facts is required: the
// assistant has no other source of truth and is instructed not to invent one.
type BreakingDraftInput struct {
	Topic  string `json:"topic" binding:"required"`
	Facts  string `json:"facts" binding:"required"`
	Source string `json:"source" binding:"required"`
	Date   string `json:"date"`
}

// BreakingDraft is the model's response, matching the shape in §22 plus an
// editorNote the model uses to flag thin source material.
type BreakingDraft struct {
	TitleKh      string `json:"titleKh"`
	TitleEn      string `json:"titleEn"`
	Category     string `json:"category"`
	SubCategory  string `json:"subCategory"`
	Summary      string `json:"summary"`
	Paragraph1   string `json:"paragraph1"`
	Paragraph2   string `json:"paragraph2"`
	TelegramPost string `json:"telegramPost"`
	EditorNote   string `json:"editorNote"`
}

// GenerateBreakingDraft produces an unpublished draft from verified facts.
func (s *Service) GenerateBreakingDraft(ctx context.Context, in BreakingDraftInput) (*BreakingDraft, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	date := in.Date
	if date == "" {
		date = time.Now().UTC().Format("2 January 2006")
	}

	prompt := strings.NewReplacer(
		"{{TOPIC}}", in.Topic,
		"{{FACTS}}", in.Facts,
		"{{SOURCE}}", in.Source,
		"{{DATE}}", date,
	).Replace(BreakingNewsPrompt)

	var out BreakingDraft
	if err := s.complete(ctx, prompt, 4096, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SEOInput carries the article text the SEO assistant works from.
type SEOInput struct {
	TitleKh  string
	TitleEn  string
	Summary  string
	Body     string
	Category string
}

// SEODraft matches §23. Every field is a suggestion for an editor to accept,
// edit or discard — the caller stores it with AIGenerated set.
type SEODraft struct {
	SEOTitle                string   `json:"seoTitle"`
	SEODescription          string   `json:"seoDescription"`
	SuggestedSlug           string   `json:"suggestedSlug"`
	OGTitle                 string   `json:"ogTitle"`
	OGDescription           string   `json:"ogDescription"`
	ImageAlt                string   `json:"imageAlt"`
	Keywords                []string `json:"keywords"`
	InternalLinkSuggestions []string `json:"internalLinkSuggestions"`
	EditorNote              string   `json:"editorNote"`
}

func (s *Service) GenerateSEO(ctx context.Context, in SEOInput) (*SEODraft, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}

	prompt := strings.NewReplacer(
		"{{TITLE_KH}}", in.TitleKh,
		"{{TITLE_EN}}", in.TitleEn,
		"{{SUMMARY}}", in.Summary,
		// The body is capped so a very long article cannot blow out the
		// request; the opening is what metadata should describe anyway.
		"{{BODY}}", utils.Truncate(utils.StripHTML(in.Body), 6000),
		"{{CATEGORY}}", in.Category,
	).Replace(SEOPrompt)

	var out SEODraft
	if err := s.complete(ctx, prompt, 2048, &out); err != nil {
		return nil, err
	}
	// Re-slugify the model's suggestion rather than trusting it to be URL-safe.
	out.SuggestedSlug = utils.Slugify(out.SuggestedSlug)
	return &out, nil
}

// SummaryDraft backs the labelled summary block in §24.
type SummaryDraft struct {
	Points     []string `json:"points"`
	EditorNote string   `json:"editorNote"`
}

func (s *Service) GenerateSummary(ctx context.Context, title, body string) (*SummaryDraft, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}

	prompt := strings.NewReplacer(
		"{{TITLE}}", title,
		"{{BODY}}", utils.Truncate(utils.StripHTML(body), 12000),
	).Replace(SummaryPrompt)

	var out SummaryDraft
	if err := s.complete(ctx, prompt, 2048, &out); err != nil {
		return nil, err
	}
	if len(out.Points) > 5 {
		out.Points = out.Points[:5]
	}
	return &out, nil
}

// complete runs one request and decodes the JSON object from the response.
//
// Streaming is used because these requests carry a full article body and can
// run long; get_final_message collapses the stream back into one response.
func (s *Service) complete(ctx context.Context, prompt string, maxTokens int64, dest any) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}

	stream := s.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(s.model),
		MaxTokens: maxTokens,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		System: []anthropic.TextBlockParam{{
			Text: systemPrompt,
			// The system prompt is byte-identical across every assistant call,
			// so caching it makes the repeat calls a journalist actually makes
			// cheaper and faster.
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})

	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return fmt.Errorf("accumulate ai response: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("ai request failed: %w", err)
	}

	// A refusal is a normal 200 response, not an error — check it before
	// reading content, or the decode below fails with a confusing message.
	if message.StopReason == anthropic.StopReasonRefusal {
		return fmt.Errorf("the assistant declined this request; rewrite the brief or draft it manually")
	}
	if message.StopReason == anthropic.StopReasonMaxTokens {
		return fmt.Errorf("the assistant's reply was cut off; shorten the source material and try again")
	}

	var text strings.Builder
	for _, block := range message.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(tb.Text)
		}
	}

	raw := extractJSON(text.String())
	if raw == "" {
		return fmt.Errorf("the assistant did not return a usable draft")
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		return fmt.Errorf("could not read the assistant's draft: %w", err)
	}
	return nil
}

// extractJSON pulls the JSON object out of a reply. The prompts ask for bare
// JSON, but a stray markdown fence or a leading sentence should not fail the
// whole request, so the outermost braces are located instead.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if fence := strings.Index(s, "```"); fence >= 0 {
		rest := s[fence+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			s = strings.TrimSpace(rest[:end])
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
