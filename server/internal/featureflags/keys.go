package featureflags

import (
	"context"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

const (
	// ComposioMCPApps gates the Composio app management UI and — together with
	// the MUL-3963 permission_mode / invocation_targets access model it depends
	// on — the aligned Private / Public-to picker in the agent create flow.
	// The access model exists to gate Composio sharing, so the two ship on the
	// same switch.
	ComposioMCPApps = "composio_mcp_apps"

	// SecretsStore gates the KHI-972 P2 encrypted workspace secret store:
	// the create/list/rotate/delete API, the CLI, and ${secret:NAME}
	// resolution at task claim time. Off by default. Checked independently
	// of ComposioMCPApps — secret resolution must apply whether or not
	// Composio MCP is enabled.
	SecretsStore = "secrets_store"
)

var frontendPublicFlags = []string{
	ComposioMCPApps,
}

func ComposioMCPAppsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, ComposioMCPApps, false)
}

// SecretsStoreEnabled reports whether the KHI-972 P2 secrets store is
// enabled for the current context.
func SecretsStoreEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, SecretsStore, false)
}

func EvaluateFrontendPublicFlags(ctx context.Context, flags *featureflag.Service) map[string]bool {
	out := make(map[string]bool, len(frontendPublicFlags))
	for _, key := range frontendPublicFlags {
		out[key] = flags.IsEnabled(ctx, key, false)
	}
	return out
}
