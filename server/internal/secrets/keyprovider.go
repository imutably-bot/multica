// Package secrets implements the KHI-972 P2 workspace secret store:
// envelope-encrypted values referenced as ${secret:NAME} and resolved only
// at task claim time (see Resolver, wired into daemon.ClaimTaskByRuntime).
package secrets

// KeyProvider wraps and unwraps the per-secret data encryption key (DEK)
// that envelope.go generates for every secret. It is the seam a future KMS
// integration replaces: EnvKeyProvider is the only implementation today
// (backed by a single static key from MULTICA_SECRETS_KEY), but a
// KMS-backed provider can satisfy the same interface — calling out to
// AWS/GCP/Vault to wrap/unwrap the DEK instead of using a local AES key —
// without any change to Encryptor, Resolver, or the handler/CLI/claim call
// sites that only ever talk to this interface.
type KeyProvider interface {
	// KeyVersion identifies the wrapping key currently used by WrapDataKey.
	// Stored alongside every envelope (workspace_secret.key_version) so a
	// later key rotation can still unwrap old rows with the old key while
	// new writes use the new one.
	KeyVersion() int32

	// WrapDataKey encrypts a freshly generated DEK under the current
	// wrapping key.
	WrapDataKey(dek []byte) ([]byte, error)

	// UnwrapDataKey decrypts a DEK that was wrapped under the given key
	// version. Returns an error if that version is not available — never
	// silently falls back to a different key.
	UnwrapDataKey(wrapped []byte, keyVersion int32) ([]byte, error)
}
