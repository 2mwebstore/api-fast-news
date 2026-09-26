// Package utils holds small shared helpers with no dependencies on the rest of
// the application.
package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	nonSlugASCII = regexp.MustCompile(`[^a-z0-9\x{1780}-\x{17FF}\x{19E0}-\x{19FF}\s-]+`)
	dashRuns     = regexp.MustCompile(`-{2,}`)
	spaceRuns    = regexp.MustCompile(`\s+`)
)

// Slugify builds a URL slug. Khmer codepoints are preserved rather than
// stripped: Khmer script is valid in a URL path (it percent-encodes), and
// dropping it would leave Khmer-only headlines with an empty slug.
//
// Callers should prefer an English title when one exists, because a
// percent-encoded Khmer slug is hard to share in plain text.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugASCII.ReplaceAllString(s, "")
	s = spaceRuns.ReplaceAllString(s, "-")
	s = dashRuns.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
		s = strings.Trim(s, "-")
	}
	return s
}

// SlugifyWithFallback returns a slug for an article, preferring the English
// title and falling back to the Khmer one. When neither yields anything
// usable it returns a dated random slug so publishing never blocks.
func SlugifyWithFallback(titleEn, titleKh, datePrefix string) string {
	if s := Slugify(titleEn); s != "" && hasASCIILetter(s) {
		return s
	}
	if s := Slugify(titleKh); s != "" {
		return s
	}
	return fmt.Sprintf("%s-%s", datePrefix, RandomHex(4))
}

func hasASCIILetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// RandomHex returns n random bytes hex-encoded, used for slug and object-key
// suffixes.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is unrecoverable for anything security-adjacent,
		// but a slug suffix can safely fall back to a fixed marker.
		return "00000000"[:n*2]
	}
	return hex.EncodeToString(b)
}

// CountWords counts words across mixed Khmer/Latin text. Khmer is written
// without spaces between words, so Khmer runs are estimated by character
// count using an average word length — good enough for a reading-time badge,
// and explicitly not presented as an exact figure.
const khmerCharsPerWord = 5

func CountWords(s string) int {
	var latinWords, khmerChars int
	inLatinWord := false

	for _, r := range s {
		switch {
		case isKhmer(r):
			khmerChars++
			inLatinWord = false
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if !inLatinWord {
				latinWords++
				inLatinWord = true
			}
		default:
			inLatinWord = false
		}
	}
	return latinWords + khmerChars/khmerCharsPerWord
}

func isKhmer(r rune) bool {
	return (r >= 0x1780 && r <= 0x17FF) || (r >= 0x19E0 && r <= 0x19FF)
}

// ReadingMinutes estimates reading time at 200 words per minute, with a floor
// of one minute.
func ReadingMinutes(words int) int {
	if words <= 0 {
		return 1
	}
	minutes := words / 200
	if words%200 != 0 {
		minutes++
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}

// Truncate shortens text to at most n runes, appending an ellipsis. It is
// rune-aware so it never splits a Khmer cluster mid-codepoint.
func Truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
