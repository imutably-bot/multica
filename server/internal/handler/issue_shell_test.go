package handler

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "claude", want: "'claude'"},
		{name: "path with spaces", value: "/tmp/my work dir", want: "'/tmp/my work dir'"},
		{name: "embedded single quote", value: "it's-a-dir", want: `'it'\''s-a-dir'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellQuote(tt.value); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestResolveShellFlavor(t *testing.T) {
	tests := []struct {
		name       string
		runtimeOS  string
		clientHint string
		want       string
	}{
		{
			// The reported bug: browsing from a non-Windows machine
			// while the runtime daemon is on Windows must not render
			// POSIX syntax just because the client guessed wrong.
			name:       "known windows runtime overrides a posix client guess",
			runtimeOS:  "windows",
			clientHint: "posix",
			want:       "powershell",
		},
		{
			name:       "known windows runtime respects an explicit cmd request",
			runtimeOS:  "windows",
			clientHint: "cmd",
			want:       "cmd",
		},
		{
			name:       "known linux runtime overrides a powershell client guess",
			runtimeOS:  "linux",
			clientHint: "powershell",
			want:       "posix",
		},
		{
			name:       "known darwin runtime overrides a powershell client guess",
			runtimeOS:  "darwin",
			clientHint: "powershell",
			want:       "posix",
		},
		{
			name:       "unknown runtime os falls back to the client hint",
			runtimeOS:  "",
			clientHint: "powershell",
			want:       "powershell",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveShellFlavor(tt.runtimeOS, tt.clientHint); got != tt.want {
				t.Errorf("resolveShellFlavor(%q, %q) = %q, want %q", tt.runtimeOS, tt.clientHint, got, tt.want)
			}
		})
	}
}

func TestPowershellQuote(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "codex", want: "'codex'"},
		{name: "path with spaces", value: `C:\Users\me\my work dir`, want: `'C:\Users\me\my work dir'`},
		{name: "embedded single quote", value: "it's-a-dir", want: "'it''s-a-dir'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := powershellQuote(tt.value); got != tt.want {
				t.Errorf("powershellQuote(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestRenderShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		shell   string
		cliName string
		args    []string
		workDir string
		want    string
	}{
		{
			name:    "posix default",
			shell:   "",
			cliName: "codex",
			args:    []string{"--no-alt-screen", "--model", "gpt-5.4-mini"},
			workDir: "/home/user/work",
			want:    "cd '/home/user/work' && 'codex' '--no-alt-screen' '--model' 'gpt-5.4-mini'",
		},
		{
			name:    "cmd",
			shell:   "cmd",
			cliName: "codex",
			args:    []string{"--no-alt-screen", "--model", "gpt-5.4-mini"},
			workDir: `C:\Users\me\work`,
			want:    `cd /d "C:\Users\me\work" && "codex" "--no-alt-screen" "--model" "gpt-5.4-mini"`,
		},
		{
			name:    "powershell",
			shell:   "powershell",
			cliName: "codex",
			args:    []string{"--no-alt-screen", "--model", "gpt-5.4-mini"},
			workDir: `C:\Users\me\work`,
			want:    `Set-Location -LiteralPath 'C:\Users\me\work'; & 'codex' '--no-alt-screen' '--model' 'gpt-5.4-mini'`,
		},
		{
			name:    "unrecognized shell falls back to posix",
			shell:   "fish",
			cliName: "claude",
			args:    []string{"--resume", "sess-1"},
			workDir: "/work",
			want:    "cd '/work' && 'claude' '--resume' 'sess-1'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderShellCommand(tt.shell, tt.cliName, tt.args, tt.workDir); got != tt.want {
				t.Errorf("renderShellCommand(%q) = %q, want %q", tt.shell, got, tt.want)
			}
		})
	}
}

func TestRenderSSHShellCommand(t *testing.T) {
	tests := []struct {
		name          string
		sshTarget     string
		remoteCommand string
		want          string
	}{
		{
			name:          "plain",
			sshTarget:     "user@gamer-pc",
			remoteCommand: `cd '/home/user/work' && 'codex' 'resume' 'sess-1'`,
			want:          `ssh user@gamer-pc -t "cd '/home/user/work' && 'codex' 'resume' 'sess-1'"`,
		},
		{
			name:          "host alias with no user@ prefix",
			sshTarget:     "gamer-pc",
			remoteCommand: `cd '/work' && 'claude'`,
			want:          `ssh gamer-pc -t "cd '/work' && 'claude'"`,
		},
		{
			name:          "embedded double quote and dollar sign are escaped for the local shell",
			sshTarget:     "user@host",
			remoteCommand: `cd '/work "quoted" $HOME' && 'claude'`,
			want:          `ssh user@host -t "cd '/work \"quoted\" \$HOME' && 'claude'"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderSSHShellCommand(tt.sshTarget, tt.remoteCommand); got != tt.want {
				t.Errorf("renderSSHShellCommand(%q, %q) = %q, want %q", tt.sshTarget, tt.remoteCommand, got, tt.want)
			}
		})
	}
}
