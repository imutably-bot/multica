package secrets

import (
	"fmt"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// EnvKeyVersion is the fixed key_version stamped on every envelope wrapped
// by EnvKeyProvider. There is only one env-sourced master key at a time —
// rotating MULTICA_SECRETS_KEY requires decrypting every row under the old
// key first (out of scope here) — so this never changes for this provider.
// A KMS provider would instead return the real key/version identifier it
// used (e.g. a KMS key ARN + version), letting multiple versions coexist.
const EnvKeyVersion int32 = 1

// EnvKeyProvider implements KeyProvider using a single static 32-byte
// master key loaded from an environment variable (MULTICA_SECRETS_KEY in
// production). It wraps secretbox.Box, the same AES-256-GCM primitive
// already used for Lark's app_secret, rather than re-implementing AES/GCM
// wiring.
type EnvKeyProvider struct {
	box *secretbox.Box
}

// NewEnvKeyProvider loads envVar as a base64-encoded 32-byte key and
// constructs the provider. Returns an error (never a zero/placeholder key)
// when the variable is unset, malformed, or the wrong length — callers
// must treat that as "secrets store disabled" per KHI-972 P2 scope item 2,
// not fall back to an implicit key.
func NewEnvKeyProvider(envVar string) (*EnvKeyProvider, error) {
	key, err := secretbox.LoadKey(envVar)
	if err != nil {
		return nil, err
	}
	box, err := secretbox.New(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: construct master key box: %w", err)
	}
	return &EnvKeyProvider{box: box}, nil
}

// KeyVersion implements KeyProvider.
func (p *EnvKeyProvider) KeyVersion() int32 { return EnvKeyVersion }

// WrapDataKey implements KeyProvider.
func (p *EnvKeyProvider) WrapDataKey(dek []byte) ([]byte, error) {
	wrapped, err := p.box.Seal(dek)
	if err != nil {
		return nil, fmt.Errorf("secrets: wrap data key: %w", err)
	}
	return wrapped, nil
}

// UnwrapDataKey implements KeyProvider. Rejects any key_version other than
// EnvKeyVersion so a row written under some future key scheme never gets
// silently (and incorrectly) opened with today's single static key.
func (p *EnvKeyProvider) UnwrapDataKey(wrapped []byte, keyVersion int32) ([]byte, error) {
	if keyVersion != EnvKeyVersion {
		return nil, fmt.Errorf("secrets: unsupported key_version %d for env key provider", keyVersion)
	}
	dek, err := p.box.Open(wrapped)
	if err != nil {
		return nil, fmt.Errorf("secrets: unwrap data key: %w", err)
	}
	return dek, nil
}
