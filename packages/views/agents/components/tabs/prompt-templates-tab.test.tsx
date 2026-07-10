// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Agent, AgentRuntime } from "@multica/core/types";

const getAgentPromptTemplates = vi.fn();

vi.mock("@multica/core/api", () => ({
  api: {
    getAgentPromptTemplates: (...args: unknown[]) => getAgentPromptTemplates(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { PromptTemplatesTab } from "./prompt-templates-tab";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function makeRuntime(provider: string): AgentRuntime {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Runtime",
    runtime_mode: "local",
    provider,
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-05-28T00:00:00Z",
    updated_at: "2026-05-28T00:00:00Z",
  };
}

beforeEach(() => {
  getAgentPromptTemplates.mockReset();
  getAgentPromptTemplates.mockResolvedValue({
    templates: [
      {
        key: "workspace_init",
        title: "Workspace Init",
        description: "Managed runtime brief preamble.",
        supported_scopes: ["workspace", "agent"],
        supported_variables: ["workspace_name"],
        default_template: "default",
        workspace_override: null,
        agent_override: null,
        base_template: "default",
        base_source: "default",
        effective_template: "default",
        effective_source: "default",
      },
    ],
  });
});

describe("PromptTemplatesTab vendor note", () => {
  it("shows CLAUDE.md for Claude-family runtimes", async () => {
    render(
      <PromptTemplatesTab
        agent={agent}
        runtime={makeRuntime("claude")}
        onSave={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(await screen.findByText(/CLAUDE\.md/i)).toBeInTheDocument();
    expect(screen.getByText(/Claude writes the managed runtime prompt/i)).toBeInTheDocument();
  });

  it("shows AGENTS.md for Hermes-family runtimes", async () => {
    render(
      <PromptTemplatesTab
        agent={agent}
        runtime={makeRuntime("hermes")}
        onSave={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(await screen.findByText(/AGENTS\.md/i)).toBeInTheDocument();
    expect(screen.getByText(/Hermes writes the managed runtime prompt/i)).toBeInTheDocument();
  });
});
