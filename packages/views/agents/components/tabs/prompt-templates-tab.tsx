"use client";

import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { Agent, AgentRuntime, PromptTemplateDescriptor } from "@multica/core/types";
import { toast } from "sonner";
import { PromptTemplatesEditor } from "../../../common/prompt-templates-editor";

function mergeRuntimeConfig(
  runtimeConfig: Record<string, unknown>,
  overrides: Record<string, string>,
): Record<string, unknown> {
  const next = { ...(runtimeConfig ?? {}) };
  next.prompt_templates = overrides;
  return next;
}

export function PromptTemplatesTab({
  agent,
  runtime,
  onSave,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  onSave: (updates: { runtime_config: Record<string, unknown> }) => Promise<void>;
}) {
  const [templates, setTemplates] = useState<PromptTemplateDescriptor[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api.getAgentPromptTemplates(agent.id)
      .then((resp) => {
        if (!cancelled) setTemplates(resp.templates);
      })
      .catch((err) => {
        if (!cancelled) toast.error(err instanceof Error ? err.message : "Failed to load prompt templates");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [agent.id]);

  const handleSave = async (overrides: Record<string, string>) => {
    await onSave({
      runtime_config: mergeRuntimeConfig(agent.runtime_config ?? {}, overrides),
    });
    const refreshed = await api.getAgentPromptTemplates(agent.id);
    setTemplates(refreshed.templates);
    toast.success("Agent prompt templates saved");
  };

  if (loading) {
    return <p className="text-sm text-muted-foreground">Loading prompt templates…</p>;
  }

  const provider = runtime?.provider ?? null;
  const managedFile = provider === "claude" || provider === "codebuddy"
    ? "CLAUDE.md"
    : provider
      ? "AGENTS.md"
      : null;
  const runtimeLabel = provider ? provider.charAt(0).toUpperCase() + provider.slice(1) : "selected runtime";
  const targetNote = managedFile
    ? `${runtimeLabel} writes the managed runtime prompt into ${managedFile}. These template overrides feed that generated file, while provider-specific sections such as skill loading and runtime instructions are still added by code for this vendor.`
    : "These template overrides feed the managed runtime prompt for the selected runtime. Provider-specific sections such as skill loading and runtime instructions are still added by code.";

  return (
    <PromptTemplatesEditor
      templates={templates}
      mode="agent"
      intro="Override individual prompt steps for this agent. Leaving a prompt inherited keeps the workspace or code default in effect."
      contextNote={targetNote}
      onSave={handleSave}
    />
  );
}
