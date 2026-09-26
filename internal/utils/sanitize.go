package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// dangerousTags are removed entirely, opening tag through closing tag.
var dangerousTags = []string{"script", "style", "iframe", "object", "embed", "form", "svg", "math"}

// blockPatterns matches a full <tag ...>...</tag> for each dangerous tag.
// Go's regexp is RE2, which has no backreferences, so each tag gets its own
// pattern rather than one pattern with a \1.
var blockPatterns = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(dangerousTags))
	for _, tag := range dangerousTags {
		out = append(out, regexp.MustCompile(
			fmt.Sprintf(`(?is)<%s\b[^>]*>.*?</\s*%s\s*>`, tag, tag),
		))
	}
	return out
}()

// danglingPattern catches an unbalanced opening or closing dangerous tag,
// which is what is left after a truncated or deliberately malformed payload.
var danglingPattern = regexp.MustCompile(
	`(?is)<\s*/?\s*(` + strings.Join(dangerousTags, "|") + `)\b[^>]*>`,
)

var (
	// Any on* attribute is an inline event handler.
	eventAttr = regexp.MustCompile(`(?is)\s+on[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`)
	// A URL attribute whose scheme can execute script.
	jsURLAttr = regexp.MustCompile(`(?is)\s+(href|src|action|formaction|xlink:href)\s*=\s*` +
		`("\s*(?:javascript|vbscript|data)\s*:[^"]*"` +
		`|'\s*(?:javascript|vbscript|data)\s*:[^']*'` +
		`|(?:javascript|vbscript|data)\s*:[^\s>]*)`)
	htmlTag      = regexp.MustCompile(`<[^>]*>`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// SanitizeHTML strips the constructs that turn stored article HTML into stored
// XSS (§74).
//
// This is a denylist over trusted-but-fallible editor output, not a general
// sanitiser for hostile input. User-submitted text (comments, tips) is stored
// and rendered as plain text via PlainText and never passes through here.
//
// Embeds the editor offers are rendered by the frontend from structured block
// data, which is why iframes can be removed outright.
func SanitizeHTML(in string) string {
	out := in
	for _, pattern := range blockPatterns {
		out = pattern.ReplaceAllString(out, "")
	}
	out = danglingPattern.ReplaceAllString(out, "")
	out = eventAttr.ReplaceAllString(out, "")
	out = jsURLAttr.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}

// StripHTML reduces HTML to its visible text, used to derive summaries, meta
// descriptions and word counts.
func StripHTML(in string) string {
	out := in
	for _, pattern := range blockPatterns {
		out = pattern.ReplaceAllString(out, " ")
	}
	out = htmlTag.ReplaceAllString(out, " ")
	out = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&#x27;", "'",
	).Replace(out)
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(out, " "))
}

// PlainText normalises user-submitted text: HTML removed, whitespace
// collapsed, length capped. Comments and tips go through this before storage.
func PlainText(in string, maxRunes int) string {
	return Truncate(StripHTML(in), maxRunes)
}
