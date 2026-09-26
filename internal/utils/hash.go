package utils

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashIP returns a salted hash of a client IP. Storing this instead of the
// address itself lets moderation rate-limit a repeat abuser without keeping
// readers' IP addresses (§43).
func HashIP(ip, salt string) string {
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(salt + "|" + ip))
	return hex.EncodeToString(sum[:16])
}
