package secrets

import (
	"errors"
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c, err := New("a-sufficiently-long-settings-key")
	if err != nil {
		t.Fatal(err)
	}

	const token = "7123456789:AAH-ExampleBotTokenValue"
	sealed, err := c.Encrypt(token)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, token) {
		t.Fatal("ciphertext contains the plaintext")
	}

	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != token {
		t.Errorf("round trip = %q, want %q", got, token)
	}
}

func TestEncryptUsesAFreshNonce(t *testing.T) {
	c, _ := New("a-sufficiently-long-settings-key")
	a, _ := c.Encrypt("same-value")
	b, _ := c.Encrypt("same-value")
	if a == b {
		t.Error("encrypting the same value twice produced identical ciphertext; the nonce is not random")
	}
}

func TestDecryptRejectsAnotherKey(t *testing.T) {
	c, _ := New("a-sufficiently-long-settings-key")
	sealed, _ := c.Encrypt("secret")

	other, _ := New("a-completely-different-settings-key")
	if _, err := other.Decrypt(sealed); err == nil {
		t.Error("a value encrypted with another key was decrypted")
	}
}

func TestDecryptRejectsPlaintext(t *testing.T) {
	c, _ := New("a-sufficiently-long-settings-key")
	// A bare value must not be handed back as though it were decrypted.
	if _, err := c.Decrypt("7123456789:AAH-PlainToken"); !errors.Is(err, ErrNotEncrypted) {
		t.Errorf("expected ErrNotEncrypted, got %v", err)
	}
}

func TestShortKeyRejected(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Error("a short key should be rejected")
	}
}

func TestMaskRevealsLittle(t *testing.T) {
	got := Mask("7123456789:AAH-ExampleBotTokenValue")
	if strings.Contains(got, "ExampleBotToken") {
		t.Errorf("mask leaked the token body: %q", got)
	}
	if Mask("") != "" {
		t.Error("an empty secret should mask to empty")
	}
	if Mask("short") != "••••••••" {
		t.Errorf("a short secret should be fully masked, got %q", Mask("short"))
	}
}
