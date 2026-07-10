"use client";

import { useEffect, useState } from "react";
import { api } from "@multica/core/api";
import type { Agent, PromptTemplateDescriptor } from "@multica/core/types";
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
  onSave,
}: {
  agent: Agent;
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

  return (
    <PromptTemplatesEditor
      templates={templates}
      mode="agent"
      intro="Override individual prompt steps for this agent. Leaving a prompt inherited keeps the workspace or code default in effect."
      onSave={handleSave}
    />
  );
}
