package utils

import "testing"

func TestParseYouTubeID(t *testing.T) {
	const id = "dQw4w9WgXcQ"

	accepted := map[string]string{
		"bare id":            id,
		"watch url":          "https://www.youtube.com/watch?v=" + id,
		"watch with params":  "https://www.youtube.com/watch?v=" + id + "&t=42s",
		"short link":         "https://youtu.be/" + id,
		"short link + time":  "https://youtu.be/" + id + "?t=42",
		"embed url":          "https://www.youtube.com/embed/" + id,
		"shorts url":         "https://www.youtube.com/shorts/" + id,
		"live url":           "https://www.youtube.com/live/" + id,
		"mobile url":         "https://m.youtube.com/watch?v=" + id,
		"nocookie embed":     "https://www.youtube-nocookie.com/embed/" + id,
		"whitespace padded":  "  https://youtu.be/" + id + "  ",
	}
	for name, input := range accepted {
		t.Run(name, func(t *testing.T) {
			if got := ParseYouTubeID(input); got != id {
				t.Errorf("ParseYouTubeID(%q) = %q, want %q", input, got, id)
			}
		})
	}

	rejected := []string{
		"", "not a url", "https://vimeo.com/12345",
		// A look-alike host must not pass: embedding from it would let someone
		// else's player run on our page.
		"https://youtube.com.evil.test/watch?v=" + id,
		"https://www.youtube.com/watch?v=tooshort",
		"javascript:alert(1)",
	}
	for _, input := range rejected {
		if got := ParseYouTubeID(input); got != "" {
			t.Errorf("ParseYouTubeID(%q) = %q, want empty", input, got)
		}
	}
}

func TestYouTubeURLsUseNoCookieHost(t *testing.T) {
	got := YouTubeEmbedURL("dQw4w9WgXcQ")
	if got != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0&modestbranding=1" {
		t.Errorf("unexpected embed URL: %q", got)
	}
	// An invalid id must not produce a URL at all.
	if YouTubeEmbedURL("../evil") != "" {
		t.Error("an invalid id produced an embed URL")
	}
	if YouTubeThumbnailURL("bad") != "" || YouTubeWatchURL("bad") != "" {
		t.Error("an invalid id produced a URL")
	}
}
