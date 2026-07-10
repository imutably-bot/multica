"use client";

import { useEffect, useState } from "react";
import { Bot } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { PromptTemplateDescriptor, Workspace } from "@multica/core/types";
import { useCurrentWorkspace } from "@multica/core/paths";
import { toast } from "sonner";
import { PromptTemplatesEditor } from "../../common/prompt-templates-editor";
import { useT } from "../../i18n";

export function AgentSettingsTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const [templates, setTemplates] = useState<PromptTemplateDescriptor[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!workspace) return;
    let cancelled = false;
    setLoading(true);
    api.getWorkspacePromptTemplates(workspace.id)
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
  }, [workspace?.id]);

  const handleSave = async (overrides: Record<string, string>) => {
    if (!workspace) return;
    const nextSettings = { ...(workspace.settings ?? {}), prompt_templates: overrides };
    const updated = await api.updateWorkspace(workspace.id, {
      settings: nextSettings,
    });
    qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
      old?.map((ws) => (ws.id === updated.id ? updated : ws)),
    );
    const refreshed = await api.getWorkspacePromptTemplates(workspace.id);
    setTemplates(refreshed.templates);
    toast.success(t(($) => $.agent_settings.toast_saved));
  };

  if (!workspace) return null;

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight flex items-center gap-2">
          <Bot className="h-5 w-5 text-muted-foreground" />
          {t(($) => $.agent_settings.title)}
        </h2>
        <p className="text-sm text-muted-foreground mt-2 max-w-3xl">
          {t(($) => $.agent_settings.description)}
        </p>
      </div>
      {loading ? (
        <p className="text-sm text-muted-foreground">Loading prompt templates…</p>
      ) : (
        <PromptTemplatesEditor
          templates={templates}
          mode="workspace"
          intro={t(($) => $.agent_settings.prompt_hint)}
          onSave={handleSave}
        />
      )}
    </div>
  );
}
