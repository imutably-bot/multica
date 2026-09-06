package protocol

import (
	"reflect"
	"testing"
)

func TestBuildInteractiveShellArgs(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		in       ShellArgsInput
		workDir  string
		extra    []string
		want     []string
	}{
		{
			name:     "claude with resume and model",
			provider: "claude",
			in: ShellArgsInput{
				Model:           "sonnet",
				IssueIdentifier: "KHI-542",
				PriorSessionID:  "sess-1",
			},
			want: []string{"--model", "sonnet", "--name", "KHI-542", "--resume", "sess-1"},
		},
		{
			name:     "codex includes resume subcommand and workdir",
			provider: "codex",
			in:       ShellArgsInput{PriorSessionID: "sess-2", Model: "gpt"},
			workDir:  "/work/dir",
			want:     []string{"resume", "sess-2", "--no-alt-screen", "-C", "/work/dir", "--model", "gpt"},
		},
		{
			// Regression: an empty workDir must not render as `-C ''`
			// (KHI-542 — Tier A copy-command emitted this before a
			// session/workdir existed for the issue).
			name:     "codex omits -C flag entirely when workdir is empty",
			provider: "codex",
			in:       ShellArgsInput{Model: "gpt-5.4-mini"},
			workDir:  "",
			want:     []string{"--no-alt-screen", "--model", "gpt-5.4-mini"},
		},
		{
			name:     "hermes prefixes chat subcommand",
			provider: "hermes",
			in:       ShellArgsInput{Model: "opus", PriorSessionID: "sess-3"},
			want:     []string{"chat", "--model", "opus", "--resume", "sess-3"},
		},
		{
			name:     "unsupported provider errors",
			provider: "unknown",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildInteractiveShellArgs(tt.provider, tt.in, tt.workDir, tt.extra)
			if tt.want == nil {
				if err == nil {
					t.Fatalf("expected error for provider %q, got args %v", tt.provider, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProviderCLIName(t *testing.T) {
	if name, ok := ProviderCLIName("codex"); !ok || name != "codex" {
		t.Errorf("codex: got (%q, %v)", name, ok)
	}
	if _, ok := ProviderCLIName("unknown"); ok {
		t.Errorf("expected unsupported provider to return ok=false")
	}
}
