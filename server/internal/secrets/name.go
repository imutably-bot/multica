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
