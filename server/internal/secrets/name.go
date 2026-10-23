package secrets

import "regexp"

// nameCharset is shared between the stored-name validator and the
// ${secret:NAME} reference parser so a secret can always be referenced by
// exactly the name it was created with — one definition, no drift.
const nameCharset = `[A-Za-z0-9_.-]{1,200}`

// NamePattern validates a secret name on create. Deliberately restrictive
// (no spaces, no braces/colons that would be ambiguous inside a
// ${secret:NAME} reference) rather than permissive-by-default.
var NamePattern = regexp.MustCompile(`^` + nameCharset + `$`)

// RefPattern matches every ${secret:NAME} occurrence inside a string,
// capturing NAME. Used by Resolver to find and substitute references
// inside mcp_config JSON and custom_env values.
var RefPattern = regexp.MustCompile(`\$\{secret:(` + nameCharset + `)\}`)

// ValidName reports whether name is an acceptable secret name.
func ValidName(name string) bool {
	return NamePattern.MatchString(name)
}

// ContainsReference reports whether raw (JSON bytes, or any other string
// content) contains at least one ${secret:NAME} occurrence. Used in two
// places that both need a cheap yes/no check without caring which names
// are referenced: the save-time stopgap in agent.go/agent_env.go (reject
// a non-owner/admin caller writing a reference at all) and the claim-time
// fail-closed guard in daemon.go (a reference present while resolution is
// unavailable must fail the task, never pass the literal text through).
func ContainsReference(raw []byte) bool {
	return RefPattern.Match(raw)
}

// StringMapContainsReference is the map[string]string (custom_env) twin of
// ContainsReference.
func StringMapContainsReference(m map[string]string) bool {
	for _, v := range m {
		if RefPattern.MatchString(v) {
			return true
		}
	}
	return false
}
