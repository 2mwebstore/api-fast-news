package utils

import (
	"net/url"
	"regexp"
	"strings"
)

// youtubeID matches the 11-character video id YouTube uses.
var youtubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ParseYouTubeID extracts the video id from anything an editor is likely to
// paste: a watch URL, a short youtu.be link, an embed URL, a Shorts link, or
// the bare id.
//
// Only the id is stored. Keeping a full pasted URL would mean trusting it at
// render time; building the embed URL ourselves means a pasted link cannot
// point the player somewhere else.
func ParseYouTubeID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if youtubeID.MatchString(input) {
		return input
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")

	switch host {
	case "youtu.be":
		return matchID(strings.TrimPrefix(parsed.Path, "/"))
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		if v := parsed.Query().Get("v"); v != "" {
			return matchID(v)
		}
		// /embed/<id>, /shorts/<id>, /live/<id>, /v/<id>
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) >= 2 {
			switch segments[0] {
			case "embed", "shorts", "live", "v":
				return matchID(segments[1])
			}
		}
	}
	return ""
}

func matchID(candidate string) string {
	// Strip anything trailing, such as a timestamp fragment.
	if i := strings.IndexAny(candidate, "?&#/"); i >= 0 {
		candidate = candidate[:i]
	}
	if youtubeID.MatchString(candidate) {
		return candidate
	}
	return ""
}

// YouTubeEmbedURL builds the privacy-enhanced embed URL.
//
// youtube-nocookie.com does not set tracking cookies until the reader actually
// plays the video, which is the right default for a news site that tells
// readers what it collects.
func YouTubeEmbedURL(id string) string {
	if !youtubeID.MatchString(id) {
		return ""
	}
	return "https://www.youtube-nocookie.com/embed/" + id + "?rel=0&modestbranding=1"
}

// YouTubeThumbnailURL returns YouTube's own poster image for a video.
func YouTubeThumbnailURL(id string) string {
	if !youtubeID.MatchString(id) {
		return ""
	}
	return "https://i.ytimg.com/vi/" + id + "/maxresdefault.jpg"
}

// YouTubeWatchURL is the canonical public link, used for attribution.
func YouTubeWatchURL(id string) string {
	if !youtubeID.MatchString(id) {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}
