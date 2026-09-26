package utils

import "testing"

func TestSanitizeHTMLRemovesScriptableConstructs(t *testing.T) {
	cases := []struct {
		name, in string
		mustNotContain []string
	}{
		{
			"script block",
			`<p>ok</p><script>alert(1)</script>`,
			[]string{"<script", "alert(1)"},
		},
		{
			"uppercase script block",
			`<P>ok</P><SCRIPT>alert(1)</SCRIPT>`,
			[]string{"SCRIPT", "alert(1)"},
		},
		{
			"unbalanced script tag",
			`<p>ok</p><script src="evil.js">`,
			[]string{"<script"},
		},
		{
			"inline event handler",
			`<p onclick="steal()">text</p>`,
			[]string{"onclick"},
		},
		{
			"event handler without quotes",
			`<img src=x onerror=alert(1)>`,
			[]string{"onerror"},
		},
		{
			"javascript url",
			`<a href="javascript:evil()">link</a>`,
			[]string{"javascript:"},
		},
		{
			"data url in src",
			`<img src="data:text/html;base64,PHNjcmlwdD4=">`,
			[]string{"data:"},
		},
		{
			"iframe",
			`<iframe src="https://evil.test"></iframe>`,
			[]string{"<iframe"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeHTML(tc.in)
			for _, forbidden := range tc.mustNotContain {
				if contains(got, forbidden) {
					t.Errorf("SanitizeHTML(%q) = %q, still contains %q", tc.in, got, forbidden)
				}
			}
		})
	}
}

func TestSanitizeHTMLKeepsLegitimateMarkup(t *testing.T) {
	in := `<h2>ចំណងជើង</h2><p>អត្ថបទ <strong>សំខាន់</strong></p><a href="https://example.com">link</a>`
	got := SanitizeHTML(in)

	for _, want := range []string{"<h2>", "<strong>", `href="https://example.com"`, "ចំណងជើង"} {
		if !contains(got, want) {
			t.Errorf("SanitizeHTML stripped legitimate markup %q from %q; got %q", want, in, got)
		}
	}
}

func TestStripHTML(t *testing.T) {
	in := `<p>ព័ត៌មាន</p><script>bad()</script><p>ថ្មី</p>`
	got := StripHTML(in)

	if contains(got, "bad()") {
		t.Errorf("StripHTML kept script contents: %q", got)
	}
	if !contains(got, "ព័ត៌មាន") || !contains(got, "ថ្មី") {
		t.Errorf("StripHTML dropped visible text: %q", got)
	}
}

func TestSlugifyPreservesKhmer(t *testing.T) {
	// Khmer must survive: a Khmer-only headline would otherwise slugify to "".
	got := Slugify("ព័ត៌មានកម្ពុជា")
	if got == "" {
		t.Fatal("Slugify returned empty for Khmer input; Khmer codepoints must be preserved")
	}
}

func TestSlugifyASCII(t *testing.T) {
	cases := map[string]string{
		"Hello World":            "hello-world",
		"  Multiple   Spaces  ":  "multiple-spaces",
		"Punctuation!@#$%":       "punctuation",
		"Already-Hyphenated":     "already-hyphenated",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugifyWithFallbackPrefersEnglish(t *testing.T) {
	got := SlugifyWithFallback("Cambodia Floods", "ទឹកជំនន់កម្ពុជា", "20260925")
	if got != "cambodia-floods" {
		t.Errorf("expected the English title to win, got %q", got)
	}
}

func TestSlugifyWithFallbackUsesDatedSlugWhenNothingUsable(t *testing.T) {
	got := SlugifyWithFallback("", "", "20260925")
	if len(got) < 9 || got[:8] != "20260925" {
		t.Errorf("expected a dated fallback slug, got %q", got)
	}
}

func TestCountWordsHandlesKhmerAndLatin(t *testing.T) {
	// Khmer has no spaces, so words are estimated from character count.
	if n := CountWords("hello world again"); n != 3 {
		t.Errorf("Latin word count = %d, want 3", n)
	}
	if n := CountWords("ព័ត៌មានកម្ពុជា"); n == 0 {
		t.Error("Khmer text counted as zero words; reading time would be wrong")
	}
}

func TestReadingMinutesFloorsAtOne(t *testing.T) {
	if got := ReadingMinutes(0); got != 1 {
		t.Errorf("ReadingMinutes(0) = %d, want 1", got)
	}
	if got := ReadingMinutes(400); got != 2 {
		t.Errorf("ReadingMinutes(400) = %d, want 2", got)
	}
}

func TestTruncateIsRuneAware(t *testing.T) {
	// Truncating by bytes would split a Khmer codepoint and produce mojibake.
	got := Truncate("ព័ត៌មានកម្ពុជាថ្មីៗ", 5)
	for _, r := range got {
		if r == '�' {
			t.Fatalf("Truncate produced a replacement character: %q", got)
		}
	}
}

func TestHashIPIsSaltedAndStable(t *testing.T) {
	a := HashIP("203.0.113.9", "salt-one")
	b := HashIP("203.0.113.9", "salt-one")
	c := HashIP("203.0.113.9", "salt-two")

	if a != b {
		t.Error("HashIP is not stable for the same IP and salt")
	}
	if a == c {
		t.Error("HashIP ignored the salt; rotating the salt must change the hash")
	}
	if contains(a, "203.0.113.9") {
		t.Error("HashIP leaked the raw address")
	}
	if HashIP("", "salt") != "" {
		t.Error("HashIP should return empty for an empty address")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
