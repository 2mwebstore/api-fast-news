package middleware

import (
	"log/slog"
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// ClientIPHeader is set by the Nuxt server on server-rendered requests so the
// API can attribute them to the reader rather than to the renderer.
const ClientIPHeader = "X-CFN-Client-IP"

// defaultTrustedProxies covers loopback and the private ranges Docker and
// Kubernetes use, which is where the Nuxt server sits in every deployment
// shape this project supports.
var defaultTrustedProxies = []string{
	"127.0.0.0/8", "::1/128",
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"fc00::/7", // unique local addresses
}

// IPResolver works out which IP a request should be billed to.
//
// Proxy headers are only honoured when the request actually arrived from a
// trusted peer. Without that check any client could send
// `CF-Connecting-IP: <someone else>` to get a fresh rate-limit quota, or to
// exhaust another visitor's.
type IPResolver struct {
	trusted []netip.Prefix
}

// NewIPResolver compiles the trusted-proxy list. Invalid entries are logged
// and skipped rather than failing startup, so a typo in configuration cannot
// take the API down — but it does narrow what is trusted, which is the safe
// direction.
func NewIPResolver(cidrs []string) *IPResolver {
	if len(cidrs) == 0 {
		cidrs = defaultTrustedProxies
	}

	resolver := &IPResolver{}
	invalid := 0

	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Accept a bare address as well as a CIDR.
		if !strings.Contains(raw, "/") {
			if addr, err := netip.ParseAddr(raw); err == nil {
				resolver.trusted = append(resolver.trusted, netip.PrefixFrom(addr, addr.BitLen()))
				continue
			}
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			slog.Warn("ignoring invalid trusted proxy entry", "value", raw, "error", err)
			invalid++
			continue
		}
		resolver.trusted = append(resolver.trusted, prefix)
	}

	// A misconfigured TRUSTED_PROXIES must not leave an empty trust list.
	// With nothing trusted, the Nuxt server's forwarded client IP is ignored
	// and every reader's page render is billed to the renderer again — the
	// whole site then shares one rate-limit quota. That failure is silent and
	// only shows up under load, so fall back to the safe default instead.
	if len(resolver.trusted) == 0 && invalid > 0 {
		slog.Error("TRUSTED_PROXIES contained no usable CIDRs; falling back to the default "+
			"(loopback and private ranges). Fix the value or leave it empty.",
			"invalid_entries", invalid)
		return NewIPResolver(nil)
	}
	return resolver
}

// ClientIP returns the address to attribute this request to.
func (r *IPResolver) ClientIP(c *gin.Context) string {
	peer := peerIP(c)

	// A request that did not come through one of our own proxies is billed to
	// whoever actually dialled us, whatever headers it carries.
	if !r.trusts(peer) {
		return peer.String()
	}

	// Our own Nuxt server, forwarding the reader it is rendering for.
	if forwarded, ok := parseIP(c.GetHeader(ClientIPHeader)); ok {
		return forwarded.String()
	}
	// Cloudflare, which overwrites this header on every request it proxies.
	if forwarded, ok := parseIP(c.GetHeader("CF-Connecting-IP")); ok {
		return forwarded.String()
	}
	// A generic proxy chain: the left-most entry is the original client.
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			if addr, ok := parseIP(part); ok {
				return addr.String()
			}
		}
	}

	return peer.String()
}

func (r *IPResolver) trusts(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	// Compare unmapped, so an IPv4-in-IPv6 peer (::ffff:127.0.0.1) still
	// matches an IPv4 prefix.
	addr = addr.Unmap()
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// peerIP is the address of whoever opened the connection.
func peerIP(c *gin.Context) netip.Addr {
	host := c.Request.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if addr, ok := parseIP(host); ok {
		return addr
	}
	return netip.Addr{}
}

func parseIP(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	// Some proxies append a port.
	if h, _, err := net.SplitHostPort(raw); err == nil {
		raw = h
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
