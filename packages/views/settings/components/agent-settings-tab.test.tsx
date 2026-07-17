import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const mockGetWorkspacePromptTemplates = vi.hoisted(() => vi.fn());
const workspaceRef = vi.hoisted(() => ({
  current: {
    id: "workspace-1",
    name: "Test Workspace",
    slug: "test-workspace",
    description: "",
    context: "",
    init_prompt: "",
    issue_prefix: "TES",
    repos: [] as { url: string }[],
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    setQueryData: vi.fn(),
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspaceRef.current,
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getWorkspacePromptTemplates: mockGetWorkspacePromptTemplates,
    updateWorkspace: mockUpdateWorkspace,
  },
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { AgentSettingsTab } from "./agent-settings-tab";

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

describe("AgentSettingsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceRef.current = {
      id: "workspace-1",
      name: "Test Workspace",
      slug: "test-workspace",
      description: "",
      context: "",
      init_prompt: "",
      issue_prefix: "TES",
      repos: [],
    };
    mockUpdateWorkspace.mockResolvedValue({
      ...workspaceRef.current,
      init_prompt: workspaceRef.current.init_prompt ?? "",
    });
    mockGetWorkspacePromptTemplates.mockResolvedValue({
      templates: [
        {
          key: "workspace_init",
          title: "Workspace init prompt",
          description: "Short prompt injected near the top of generated AGENTS.md / CLAUDE.md workdir guidance.",
          supported_scopes: ["workspace", "agent"],
          supported_variables: ["agent_name", "workspace_name", "task_type"],
          default_template: "You are {{agent_name}}",
          workspace_override: null,
          base_template: "You are {{agent_name}}",
          base_source: "default",
          effective_template: "You are {{agent_name}}",
          effective_source: "default",
        },
      ],
    });
  });

  it("loads the default prompt template and shows the supported variables", async () => {
    render(<AgentSettingsTab />, { wrapper: I18nWrapper });

    const prompt = await screen.findByDisplayValue("You are {{agent_name}}");
    expect(prompt).toBeTruthy();
    expect(screen.getByText("{{agent_name}}")).toBeTruthy();
    expect(screen.getByText("{{workspace_name}}")).toBeTruthy();
    expect(screen.getByText("{{task_type}}")).toBeTruthy();
  });

  it("saves the edited prompt back to the workspace", async () => {
    const user = userEvent.setup();
    render(<AgentSettingsTab />, { wrapper: I18nWrapper });

    const prompt = (await screen.findByDisplayValue("You are {{agent_name}}")) as HTMLTextAreaElement;
    await user.clear(prompt);
    await user.type(prompt, "Custom prompt");

    await user.click(screen.getByRole("button", { name: "Save prompts" }));

    await waitFor(() => {
      expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1);
    });
    expect(mockUpdateWorkspace).toHaveBeenCalledWith("workspace-1", {
      settings: { prompt_templates: { workspace_init: "Custom prompt" } },
    });
  });
});
