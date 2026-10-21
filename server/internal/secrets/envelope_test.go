package secrets

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func randomBase64Key(t *testing.T) string {
	t.Helper()
	key := make([]byte, dekSize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate random key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

// TestEncryptDecryptRoundTrip covers KHI-972 P2 test item "encryption
// round trip": a value encrypted under the current master key decrypts
// back to the exact original bytes, and the stored envelope never
// contains the plaintext as a readable substring.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc := testEncryptor(t, "SECRETS_TEST_KEY_ROUNDTRIP")

	plaintext := []byte("sk-example-super-secret-value-12345")
	envelope, keyVersion, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if keyVersion != EnvKeyVersion {
		t.Fatalf("keyVersion = %d, want %d", keyVersion, EnvKeyVersion)
	}
	if bytes.Contains(envelope, plaintext) {
		t.Fatalf("envelope contains the plaintext verbatim: %x", envelope)
	}

	got, err := enc.Decrypt(envelope, keyVersion)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt = %q, want %q", got, plaintext)
	}
}

// TestEncryptIsNonDeterministic ensures two Encrypt calls on the same
// plaintext never produce the same envelope (fresh DEK + fresh nonce each
// time), so an attacker with read access to the table cannot correlate
// two secrets by comparing ciphertext bytes.
func TestEncryptIsNonDeterministic(t *testing.T) {
	enc := testEncryptor(t, "SECRETS_TEST_KEY_NONDETERMINISTIC")
	a, _, err := enc.Encrypt([]byte("same-value"))
	if err != nil {
		t.Fatalf("Encrypt #1: %v", err)
	}
	b, _, err := enc.Encrypt([]byte("same-value"))
	if err != nil {
		t.Fatalf("Encrypt #2: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatalf("two Encrypt calls on the same plaintext produced identical envelopes")
	}
}

// TestDecryptWithWrongMasterKeyFails covers KHI-972 P2 test item "wrong
// master key": a value encrypted under one master key must not decrypt
// under a different one, and the resulting error must never leak the
// plaintext.
func TestDecryptWithWrongMasterKeyFails(t *testing.T) {
	encA := testEncryptor(t, "SECRETS_TEST_KEY_A")
	encB := testEncryptor(t, "SECRETS_TEST_KEY_B")

	plaintext := []byte("do-not-leak-me")
	envelope, keyVersion, err := encA.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt under key A: %v", err)
	}

	_, err = encB.Decrypt(envelope, keyVersion)
	if err == nil {
		t.Fatalf("Decrypt under the wrong master key unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), string(plaintext)) {
		t.Fatalf("decrypt error leaks the plaintext: %v", err)
	}
}

// TestDecryptMalformedEnvelope exercises the structural guards in Decrypt
// (too short / truncated wrapped-key section / bad format tag) rather
// than the AEAD authentication path.
func TestDecryptMalformedEnvelope(t *testing.T) {
	enc := testEncryptor(t, "SECRETS_TEST_KEY_MALFORMED")

	cases := map[string][]byte{
		"empty":              {},
		"too short":          {1, 0},
		"bad format version": {9, 0, 0},
		"truncated wrapped":  {1, 0, 50},
	}
	for name, envelope := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := enc.Decrypt(envelope, EnvKeyVersion); err == nil {
				t.Fatalf("Decrypt(%q) unexpectedly succeeded", name)
			}
		})
	}
}

// TestDecryptUnsupportedKeyVersion ensures a row stamped with a
// key_version the current provider doesn't recognize fails loudly instead
// of being silently opened with the wrong key.
func TestDecryptUnsupportedKeyVersion(t *testing.T) {
	enc := testEncryptor(t, "SECRETS_TEST_KEY_VERSION")
	envelope, _, err := enc.Encrypt([]byte("value"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := enc.Decrypt(envelope, 99); err == nil {
		t.Fatalf("Decrypt with unsupported key_version unexpectedly succeeded")
	}
}

// TestNewEnvKeyProvider_MissingOrInvalidKey covers scope item 2: "If the
// key is missing the feature stays disabled" — construction must fail
// clearly rather than falling back to a zero/implicit key.
func TestNewEnvKeyProvider_MissingOrInvalidKey(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if _, err := NewEnvKeyProvider("SECRETS_TEST_KEY_DOES_NOT_EXIST"); err == nil {
			t.Fatalf("expected an error for an unset env var")
		}
	})
	t.Run("wrong length", func(t *testing.T) {
		t.Setenv("SECRETS_TEST_KEY_SHORT", base64.StdEncoding.EncodeToString([]byte("too-short")))
		if _, err := NewEnvKeyProvider("SECRETS_TEST_KEY_SHORT"); err == nil {
			t.Fatalf("expected an error for a key that is not 32 bytes")
		}
	})
	t.Run("not base64", func(t *testing.T) {
		t.Setenv("SECRETS_TEST_KEY_BADB64", "not-valid-base64!!!")
		if _, err := NewEnvKeyProvider("SECRETS_TEST_KEY_BADB64"); err == nil {
			t.Fatalf("expected an error for invalid base64")
		}
	})
}
