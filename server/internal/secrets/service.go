package secrets

// Service bundles the two things the handler layer and the claim path
// need, both built from the same KeyProvider: Encryptor for CRUD
// (create/rotate encrypt the value; read paths decrypt it) and Resolver
// for claim-time ${secret:NAME} substitution. One construction site keeps
// handler.Handler.Secrets and daemon.go's claim-time resolution sharing
// identical crypto configuration.
type Service struct {
	*Encryptor
	*Resolver
}

// NewService constructs a Service from a KeyProvider — EnvKeyProvider
// today, a KMS-backed implementation later without any call-site change.
func NewService(kp KeyProvider) *Service {
	enc := NewEncryptor(kp)
	return &Service{
		Encryptor: enc,
		Resolver:  NewResolver(enc),
	}
}
