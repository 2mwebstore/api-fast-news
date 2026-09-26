// Package secrets encrypts configuration values that are stored in the
// database, so a database dump does not hand over the newsroom's bot token.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var ErrNotEncrypted = errors.New("value is not encrypted")

// Cipher encrypts and decrypts settings values with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// New derives a key from the supplied secret.
//
// The secret should come from SETTINGS_KEY. It falls back to JWT_SECRET so a
// deployment works without a second variable — but the two are then coupled:
// rotating JWT_SECRET makes existing encrypted settings unreadable, and they
// have to be re-entered. Set SETTINGS_KEY explicitly to decouple them.
func New(secret string) (*Cipher, error) {
	if len(secret) < 16 {
		return nil, fmt.Errorf("settings encryption key is too short")
	}
	// SHA-256 gives a fixed 32-byte key from a variable-length secret.
	sum := sha256.Sum256([]byte("cfn-settings-v1|" + secret))

	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

const prefix = "enc:v1:"

// Encrypt returns a self-describing, base64 ciphertext. The nonce is random
// per call and stored with the ciphertext.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A value without the prefix is rejected rather than
// returned as-is, so a failed migration cannot silently leak a plaintext
// token into a place that expects ciphertext.
func (c *Cipher) Decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return "", ErrNotEncrypted
	}
	raw, err := base64.StdEncoding.DecodeString(value[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(raw) < c.aead.NonceSize() {
		return "", fmt.Errorf("ciphertext is too short")
	}
	nonce, body := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]

	plaintext, err := c.aead.Open(nil, nonce, body, nil)
	if err != nil {
		// Usually a changed key. Say so, because the fix is to re-enter the
		// value, not to debug the cipher.
		return "", fmt.Errorf("could not decrypt: the settings key may have changed since this value was saved")
	}
	return string(plaintext), nil
}

// Mask renders a secret for display: enough to recognise, not enough to use.
func Mask(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	runes := []rune(plaintext)
	if len(runes) <= 8 {
		return "••••••••"
	}
	return string(runes[:4]) + "••••••••" + string(runes[len(runes)-4:])
}
