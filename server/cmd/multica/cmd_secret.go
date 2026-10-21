package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Secret commands — KHI-972 P2 workspace secret store.
//
// Every mutating command reads the value from stdin only (--stdin), never
// as a flag argument: a value on the command line is visible in shell
// history and 'ps' / /proc/<pid>/cmdline for the life of the process,
// which defeats the point of an encrypted store. There is deliberately no
// --value flag here, unlike --custom-env's inline/stdin/file trio
// elsewhere in this CLI — secrets get the strictest of those three modes
// and nothing else.
// ---------------------------------------------------------------------------

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Work with the workspace secret store",
}

var secretSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Create a new secret, reading its value from stdin",
	Args:  exactArgs(1),
	RunE:  runSecretSet,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets in the workspace (names and metadata only — values are never returned)",
	RunE:  runSecretList,
}

var secretRotateCmd = &cobra.Command{
	Use:   "rotate <name>",
	Short: "Replace a secret's value, reading the new value from stdin",
	Args:  exactArgs(1),
	RunE:  runSecretRotate,
}

var secretDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a secret",
	Args:  exactArgs(1),
	RunE:  runSecretDelete,
}

func init() {
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretRotateCmd)
	secretCmd.AddCommand(secretDeleteCmd)

	secretSetCmd.Flags().Bool("stdin", false, "Read the secret value from stdin (required)")
	secretSetCmd.Flags().String("description", "", "Optional human-readable description")
	secretSetCmd.Flags().String("output", "json", "Output format: table or json")

	secretListCmd.Flags().String("output", "table", "Output format: table or json")
	secretListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	secretRotateCmd.Flags().Bool("stdin", false, "Read the new secret value from stdin (required)")
	secretRotateCmd.Flags().String("output", "json", "Output format: table or json")

	secretDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

// readSecretValueFromStdin enforces --stdin as the only accepted input
// channel and reads the value, trimming exactly one trailing newline (the
// kind `echo`/a text editor adds) without touching other whitespace that
// might be meaningful inside the secret itself.
func readSecretValueFromStdin(cmd *cobra.Command) (string, error) {
	fromStdin, _ := cmd.Flags().GetBool("stdin")
	if !fromStdin {
		return "", fmt.Errorf("--stdin is required: pipe the secret value in, e.g. printf '%%s' \"$VALUE\" | multica secret set NAME --stdin")
	}
	buf, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", fmt.Errorf("read secret value from stdin: %w", err)
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(buf), "\n"), "\r")
	if value == "" {
		return "", fmt.Errorf("stdin was empty: no secret value to set")
	}
	return value, nil
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	name := args[0]
	value, err := readSecretValueFromStdin(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"name": name, "value": value}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		body["description"] = desc
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/secrets", body, &result); err != nil {
		return fmt.Errorf("create secret: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printSecretTable([]map[string]any{result}, false)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/secrets", &result); err != nil {
		return fmt.Errorf("list secrets: %w", err)
	}
	secretsRaw, _ := result["secrets"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, secretsRaw)
	}

	rows := make([]map[string]any, 0, len(secretsRaw))
	for _, raw := range secretsRaw {
		if m, ok := raw.(map[string]any); ok {
			rows = append(rows, m)
		}
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printSecretTable(rows, fullID)
	return nil
}

func runSecretRotate(cmd *cobra.Command, args []string) error {
	name := args[0]
	value, err := readSecretValueFromStdin(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/secrets/"+name+"/rotate", map[string]any{"value": value}, &result); err != nil {
		return fmt.Errorf("rotate secret: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printSecretTable([]map[string]any{result}, false)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runSecretDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/secrets/"+name); err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"name": name, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Secret %s deleted.\n", name)
	return nil
}

func printSecretTable(rows []map[string]any, fullID bool) {
	headers := []string{"ID", "NAME", "KEY_VERSION", "DESCRIPTION", "ROTATED_AT", "CREATED_AT"}
	out := make([][]string, 0, len(rows))
	for _, s := range rows {
		created := strVal(s, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rotated := strVal(s, "rotated_at")
		if len(rotated) >= 10 {
			rotated = rotated[:10]
		}
		out = append(out, []string{
			displayID(strVal(s, "id"), fullID),
			strVal(s, "name"),
			strVal(s, "key_version"),
			strVal(s, "description"),
			rotated,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
}
