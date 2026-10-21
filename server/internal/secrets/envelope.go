package secrets

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// dekSize is the data-encryption-key length: AES-256 wants 32 bytes.
const dekSize = 32

// envelopeFormatVersion is a leading format tag on every stored envelope,
// independent of KeyProvider.KeyVersion. It exists so a future change to
// the on-disk layout (e.g. a different AEAD, or additional metadata) can
// be detected and rejected explicitly instead of mis-parsed.
const envelopeFormatVersion byte = 1

// ErrEnvelopeMalformed covers any structural problem with a stored
// envelope (too short, bad format tag, truncated wrapped-key section) —
// kept distinct from the underlying AEAD "authentication failed" error so
// callers (and tests) can tell "this isn't an envelope at all" from "this
// envelope's key or value field failed to decrypt".
var ErrEnvelopeMalformed = errors.New("secrets: malformed envelope")

// Encryptor performs envelope encryption for secret values: a random DEK
// encrypts the value, and KeyProvider wraps/unwraps that DEK. Safe for
// concurrent use — KeyProvider implementations and secretbox.Box both are.
type Encryptor struct {
	kp KeyProvider
}

// NewEncryptor constructs an Encryptor bound to the given KeyProvider.
func NewEncryptor(kp KeyProvider) *Encryptor {
	return &Encryptor{kp: kp}
}

// Encrypt seals plaintext under a freshly generated per-call data key and
// returns the stored envelope bytes plus the key_version to persist
// alongside it. The DEK itself is never returned or logged; it only ever
// exists wrapped (in the envelope) or as a local byte slice zeroed before
// this function returns.
func (e *Encryptor) Encrypt(plaintext []byte) (envelope []byte, keyVersion int32, err error) {
	dek := make([]byte, dekSize)
	if _, err := rand.Read(dek); err != nil {
		return nil, 0, fmt.Errorf("secrets: generate data key: %w", err)
	}
	defer zero(dek)

	dekBox, err := secretbox.New(dek)
	if err != nil {
		return nil, 0, fmt.Errorf("secrets: construct data key box: %w", err)
	}
	ciphertext, err := dekBox.Seal(plaintext)
	if err != nil {
		return nil, 0, fmt.Errorf("secrets: seal value: %w", err)
	}

	wrapped, err := e.kp.WrapDataKey(dek)
	if err != nil {
		return nil, 0, fmt.Errorf("secrets: wrap data key: %w", err)
	}
	if len(wrapped) > 0xFFFF {
		return nil, 0, fmt.Errorf("secrets: wrapped data key too large (%d bytes)", len(wrapped))
	}

	out := make([]byte, 0, 1+2+len(wrapped)+len(ciphertext))
	out = append(out, envelopeFormatVersion)
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(wrapped)))
	out = append(out, lenBuf[:]...)
	out = append(out, wrapped...)
	out = append(out, ciphertext...)
	return out, e.kp.KeyVersion(), nil
}

// Decrypt reverses Encrypt. keyVersion must match the version recorded at
// encrypt time (workspace_secret.key_version); KeyProvider.UnwrapDataKey
// decides whether it can still service that version. Every error path is
// a generic, value-free message — the caller (Resolver) wraps these with
// the secret's NAME only, never with any decrypted or partially-decrypted
// byte content, satisfying "no secret value in logs, errors, API
// responses" even on the failure path.
func (e *Encryptor) Decrypt(envelope []byte, keyVersion int32) ([]byte, error) {
	if len(envelope) < 3 {
		return nil, ErrEnvelopeMalformed
	}
	if envelope[0] != envelopeFormatVersion {
		return nil, fmt.Errorf("%w: unsupported format version %d", ErrEnvelopeMalformed, envelope[0])
	}
	wrappedLen := int(binary.BigEndian.Uint16(envelope[1:3]))
	if len(envelope) < 3+wrappedLen {
		return nil, fmt.Errorf("%w: truncated wrapped key", ErrEnvelopeMalformed)
	}
	wrapped := envelope[3 : 3+wrappedLen]
	ciphertext := envelope[3+wrappedLen:]

	dek, err := e.kp.UnwrapDataKey(wrapped, keyVersion)
	if err != nil {
		return nil, fmt.Errorf("secrets: unwrap data key: %w", err)
	}
	defer zero(dek)

	dekBox, err := secretbox.New(dek)
	if err != nil {
		return nil, fmt.Errorf("secrets: construct data key box: %w", err)
	}
	plaintext, err := dekBox.Open(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("secrets: open value: %w", err)
	}
	return plaintext, nil
}

// zero best-effort clears a byte slice holding key material. Go's GC may
// still retain copies made before this call (e.g. inside secretbox.New's
// aes.NewCipher key schedule), so this is defense-in-depth, not a
// guarantee — it shrinks the window a DEK is readable from a heap dump,
// nothing more.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
