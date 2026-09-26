package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func requestFrom(peer string, headers map[string]string) *gin.Context {
	req := httptest.NewRequest(http.MethodGet, "/api/news", nil)
	req.RemoteAddr = peer
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestClientIPIgnoresHeadersFromUntrustedPeers(t *testing.T) {
	resolver := NewIPResolver(nil)

	// The attack this prevents: a client on the open internet claiming to be
	// someone else, either to get a fresh quota or to exhaust another
	// visitor's.
	spoofed := map[string]string{
		ClientIPHeader:     "1.2.3.4",
		"CF-Connecting-IP": "5.6.7.8",
		"X-Forwarded-For":  "9.10.11.12",
	}
	got := resolver.ClientIP(requestFrom("203.0.113.9:44321", spoofed))
	if got != "203.0.113.9" {
		t.Errorf("ClientIP() = %q, want the real peer 203.0.113.9 — headers from an untrusted peer must be ignored", got)
	}
}

func TestClientIPHonoursForwardingFromTrustedPeers(t *testing.T) {
	resolver := NewIPResolver(nil)

	cases := []struct {
		name, peer string
		headers    map[string]string
		want       string
	}{
		{
			"nuxt server forwarding a reader",
			"127.0.0.1:51000",
			map[string]string{ClientIPHeader: "203.0.113.9"},
			"203.0.113.9",
		},
		{
			"cloudflare",
			"10.1.2.3:51000",
			map[string]string{"CF-Connecting-IP": "203.0.113.20"},
			"203.0.113.20",
		},
		{
			"x-forwarded-for chain uses the left-most entry",
			"172.18.0.5:51000",
			map[string]string{"X-Forwarded-For": "203.0.113.30, 10.0.0.1, 10.0.0.2"},
			"203.0.113.30",
		},
		{
			"trusted peer with no forwarding headers",
			"127.0.0.1:51000",
			nil,
			"127.0.0.1",
		},
		{
			"ipv6 loopback is trusted",
			"[::1]:51000",
			map[string]string{ClientIPHeader: "203.0.113.40"},
			"203.0.113.40",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.ClientIP(requestFrom(tc.peer, tc.headers)); got != tc.want {
				t.Errorf("ClientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPPrefersOurOwnHeaderOverCloudflare(t *testing.T) {
	// Both are present when Cloudflare fronts the Nuxt server. Ours is the
	// more specific signal: it names the reader this render is for.
	resolver := NewIPResolver(nil)
	got := resolver.ClientIP(requestFrom("127.0.0.1:51000", map[string]string{
		ClientIPHeader:     "203.0.113.9",
		"CF-Connecting-IP": "198.51.100.1",
	}))
	if got != "203.0.113.9" {
		t.Errorf("ClientIP() = %q, want 203.0.113.9", got)
	}
}

// A misconfigured value must not silently empty the trust list: that would
// make the Nuxt server's forwarded IP be ignored, and the whole site would
// share one rate-limit quota.
func TestNewIPResolverFallsBackWhenEveryEntryIsInvalid(t *testing.T) {
	resolver := NewIPResolver([]string{"true"})

	got := resolver.ClientIP(requestFrom("127.0.0.1:51000", map[string]string{ClientIPHeader: "203.0.113.9"}))
	if got != "203.0.113.9" {
		t.Errorf("ClientIP() = %q, want 203.0.113.9 — a bad TRUSTED_PROXIES must fall back to the default", got)
	}

	// The fallback must still reject headers from a genuinely untrusted peer.
	spoofed := resolver.ClientIP(requestFrom("203.0.113.200:51000", map[string]string{ClientIPHeader: "1.2.3.4"}))
	if spoofed != "203.0.113.200" {
		t.Errorf("fallback trusted an untrusted peer: got %q", spoofed)
	}
}

func TestNewIPResolverAcceptsBareAddressesAndSkipsGarbage(t *testing.T) {
	resolver := NewIPResolver([]string{"198.51.100.7", "not-an-ip", "203.0.113.0/24"})

	if got := resolver.ClientIP(requestFrom("198.51.100.7:1", map[string]string{ClientIPHeader: "1.1.1.1"})); got != "1.1.1.1" {
		t.Errorf("a bare trusted address was not honoured, got %q", got)
	}
	if got := resolver.ClientIP(requestFrom("203.0.113.55:1", map[string]string{ClientIPHeader: "1.1.1.1"})); got != "1.1.1.1" {
		t.Errorf("a trusted CIDR was not honoured, got %q", got)
	}
	// The invalid entry must not have widened what is trusted.
	if got := resolver.ClientIP(requestFrom("192.0.2.1:1", map[string]string{ClientIPHeader: "1.1.1.1"})); got != "192.0.2.1" {
		t.Errorf("an untrusted peer was honoured, got %q", got)
	}
}
