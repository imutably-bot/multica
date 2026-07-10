"use client";

import { useEffect, useState } from "react";
import { Bot, Loader2, Save } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import { Separator } from "@multica/ui/components/ui/separator";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useT } from "../../i18n";

const DEFAULT_INIT_PROMPT =
  "You are {{agent_name}}, an AI agent that helps users in {{workspace_name}}. Be concise, helpful, and action-oriented. Use available tools when needed and ask clarifying questions if something is unclear.";

const SUPPORTED_VARIABLES = [
  "{{agent_name}}",
  "{{workspace_name}}",
  "{{issue_id}}",
  "{{chat_id}}",
  "{{user_name}}",
  "{{repo_url}}",
  "{{task_type}}",
] as const;

export function AgentSettingsTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const [prompt, setPrompt] = useState(DEFAULT_INIT_PROMPT);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setPrompt(workspace?.init_prompt?.trim() || DEFAULT_INIT_PROMPT);
  }, [workspace?.id, workspace?.init_prompt]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { init_prompt: prompt });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      setPrompt(updated.init_prompt?.trim() || DEFAULT_INIT_PROMPT);
      toast.success(t(($) => $.agent_settings.toast_saved));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.agent_settings.toast_save_failed));
    } finally {
      setSaving(false);
    }
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

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1.35fr)_minmax(280px,0.65fr)]">
        <Card>
          <CardHeader className="space-y-2">
            <CardTitle className="text-sm">{t(($) => $.agent_settings.prompt_label)}</CardTitle>
            <CardDescription>{t(($) => $.agent_settings.prompt_hint)}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="workspace-init-prompt" className="sr-only">
                {t(($) => $.agent_settings.prompt_label)}
              </Label>
              <Textarea
                id="workspace-init-prompt"
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                className="min-h-48 font-mono text-sm leading-6"
                placeholder={DEFAULT_INIT_PROMPT}
              />
            </div>

            <div className="flex items-center justify-between gap-3">
              <p className="text-xs text-muted-foreground max-w-xl">
                {t(($) => $.agent_settings.prompt_footer)}
              </p>
              <Button onClick={handleSave} disabled={saving}>
                {saving ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    {t(($) => $.agent_settings.saving)}
                  </>
                ) : (
                  <>
                    <Save className="h-4 w-4" />
                    {t(($) => $.agent_settings.save)}
                  </>
                )}
              </Button>
            </div>
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card>
            <CardHeader className="space-y-2">
              <CardTitle className="text-sm">{t(($) => $.agent_settings.supported_variables)}</CardTitle>
              <CardDescription>{t(($) => $.agent_settings.supported_variables_hint)}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-2">
              {SUPPORTED_VARIABLES.map((variable) => (
                <Badge key={variable} variant="secondary" className="font-mono">
                  {variable}
                </Badge>
              ))}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="space-y-2">
              <CardTitle className="text-sm">{t(($) => $.agent_settings.help_title)}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>{t(($) => $.agent_settings.help_short)}</p>
              <Separator />
              <p>{t(($) => $.agent_settings.help_files)}</p>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  );
}
