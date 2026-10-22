package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Agent secret grant commands — KHI-972 P2 review fix (finding 1).
//
// Grants are the allow-list that decides which workspace secrets a given
// agent's ${secret:NAME} references may resolve to at claim time. Owner
// or admin only (server-enforced); there is no inline secret VALUE here
// at all — only the secret's name — so none of the --stdin plumbing the
// `secret` commands need applies.
// ---------------------------------------------------------------------------

var agentSecretsCmd = &cobra.Command{
	Use:   "secrets",
	Short: "Manage which workspace secrets an agent is granted (owner/admin only)",
}

var agentSecretsGrantCmd = &cobra.Command{
	Use:   "grant <agent-id> <secret-name>",
	Short: "Grant an agent access to resolve a workspace secret at claim time",
	Args:  exactArgs(2),
	RunE:  runAgentSecretsGrant,
}

var agentSecretsRevokeCmd = &cobra.Command{
	Use:   "revoke <agent-id> <secret-name>",
	Short: "Revoke an agent's access to a workspace secret",
	Args:  exactArgs(2),
	RunE:  runAgentSecretsRevoke,
}

var agentSecretsListCmd = &cobra.Command{
	Use:   "list <agent-id>",
	Short: "List the secrets an agent is granted",
	Args:  exactArgs(1),
	RunE:  runAgentSecretsList,
}

func init() {
	agentCmd.AddCommand(agentSecretsCmd)
	agentSecretsCmd.AddCommand(agentSecretsGrantCmd)
	agentSecretsCmd.AddCommand(agentSecretsRevokeCmd)
	agentSecretsCmd.AddCommand(agentSecretsListCmd)

	agentSecretsGrantCmd.Flags().String("output", "json", "Output format: table or json")
	agentSecretsRevokeCmd.Flags().String("output", "json", "Output format: table or json")
	agentSecretsListCmd.Flags().String("output", "table", "Output format: table or json")
}

func runAgentSecretsGrant(cmd *cobra.Command, args []string) error {
	agentID, secretName := args[0], args[1]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/agents/"+agentID+"/secrets", map[string]any{"secret_name": secretName}, &result); err != nil {
		return fmt.Errorf("grant agent secret: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printAgentSecretGrantTable([]map[string]any{result})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runAgentSecretsRevoke(cmd *cobra.Command, args []string) error {
	agentID, secretName := args[0], args[1]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/agents/"+agentID+"/secrets/"+secretName); err != nil {
		return fmt.Errorf("revoke agent secret: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"agent_id": agentID, "secret_name": secretName, "revoked": true})
	}
	fmt.Fprintf(os.Stdout, "Revoked %s from agent %s.\n", secretName, agentID)
	return nil
}

func runAgentSecretsList(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+agentID+"/secrets", &result); err != nil {
		return fmt.Errorf("list agent secret grants: %w", err)
	}
	grantsRaw, _ := result["grants"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, grantsRaw)
	}

	rows := make([]map[string]any, 0, len(grantsRaw))
	for _, raw := range grantsRaw {
		if m, ok := raw.(map[string]any); ok {
			rows = append(rows, m)
		}
	}
	printAgentSecretGrantTable(rows)
	return nil
}

func printAgentSecretGrantTable(rows []map[string]any) {
	headers := []string{"ID", "AGENT_ID", "SECRET_NAME", "CREATED_AT"}
	out := make([][]string, 0, len(rows))
	for _, g := range rows {
		created := strVal(g, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		out = append(out, []string{
			strVal(g, "id"),
			strVal(g, "agent_id"),
			strVal(g, "secret_name"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, out)
}
