package protocol

import "fmt"

// ShellArgsInput carries the session fields needed to compose an
// interactive CLI invocation for a given provider. It intentionally
// excludes daemon-local extras (per-profile fixed args, workspace
// defaults) that only the daemon knows about — callers pass those in
// separately as extraArgs.
type ShellArgsInput struct {
	Model           string
	ThinkingLevel   string
	IssueIdentifier string
	PriorSessionID  string
	CustomArgs      []string
}

// BuildInteractiveShellArgs returns the CLI arguments used to open an
// interactive session for the given provider. Both the daemon (to
// actually launch the process) and the server (to render a copyable
// command for the user's own terminal) call this so the two stay in
// sync.
func BuildInteractiveShellArgs(provider string, in ShellArgsInput, workDir string, extraArgs []string) ([]string, error) {
	args := append([]string{}, extraArgs...)
	switch provider {
	case "claude", "codebuddy":
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		if in.ThinkingLevel != "" {
			args = append(args, "--effort", in.ThinkingLevel)
		}
		if in.IssueIdentifier != "" {
			args = append(args, "--name", in.IssueIdentifier)
		}
		if in.PriorSessionID != "" {
			args = append(args, "--resume", in.PriorSessionID)
		}
		args = append(args, in.CustomArgs...)
		return args, nil
	case "codex":
		if in.PriorSessionID != "" {
			args = append([]string{"resume", in.PriorSessionID}, args...)
		}
		args = append(args, "--no-alt-screen")
		if workDir != "" {
			args = append(args, "-C", workDir)
		}
		if in.Model != "" {
			args = append(args, "--model", in.Model)
		}
		args = append(args, in.CustomArgs...)
		return args, nil
	case "hermes":
		// "chat" subcommand must come before all flags for interactive use.
		hermesArgs := append([]string{"chat"}, args...)
		if in.Model != "" {
			hermesArgs = append(hermesArgs, "--model", in.Model)
		}
		if in.PriorSessionID != "" {
			hermesArgs = append(hermesArgs, "--resume", in.PriorSessionID)
		}
		hermesArgs = append(hermesArgs, in.CustomArgs...)
		return hermesArgs, nil
	default:
		return nil, fmt.Errorf("provider %q does not support issue shell yet", provider)
	}
}

// ProviderCLIName returns the CLI binary name a provider's interactive
// session is launched with. Used to render a copyable command; the
// daemon's actual launch path may differ (e.g. a custom profile's
// absolute path), which only the daemon machine knows about.
func ProviderCLIName(provider string) (string, bool) {
	switch provider {
	case "claude", "codebuddy", "codex", "hermes":
		return provider, true
	default:
		return "", false
	}
}
