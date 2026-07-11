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
