"use client";

import { useEffect, useMemo, useState } from "react";
import type { PromptTemplateDescriptor } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Textarea } from "@multica/ui/components/ui/textarea";

type Mode = "workspace" | "agent";

function initialDrafts(templates: PromptTemplateDescriptor[], mode: Mode): Record<string, string> {
  const out: Record<string, string> = {};
  for (const template of templates) {
    if (mode === "workspace") {
      out[template.key] = template.workspace_override ?? template.default_template;
    } else {
      out[template.key] = template.agent_override ?? template.base_template;
    }
  }
  return out;
}

function buildOverrides(
  templates: PromptTemplateDescriptor[],
  drafts: Record<string, string>,
  mode: Mode,
): Record<string, string> {
  const out: Record<string, string> = {};
  for (const template of templates) {
    const next = drafts[template.key] ?? "";
    const baseline = mode === "workspace" ? template.default_template : template.base_template;
    if (next !== baseline) {
      out[template.key] = next;
    }
  }
  return out;
}

export function PromptTemplatesEditor({
  templates,
  mode,
  onSave,
  intro,
  contextNote,
}: {
  templates: PromptTemplateDescriptor[];
  mode: Mode;
  onSave: (overrides: Record<string, string>) => Promise<void>;
  intro?: string;
  contextNote?: string;
}) {
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDrafts(initialDrafts(templates, mode));
  }, [templates, mode]);

  const dirty = useMemo(() => {
    const next = buildOverrides(templates, drafts, mode);
    const current = buildOverrides(templates, initialDrafts(templates, mode), mode);
    return JSON.stringify(next) !== JSON.stringify(current);
  }, [drafts, mode, templates]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await onSave(buildOverrides(templates, drafts, mode));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      {intro ? <p className="text-sm text-muted-foreground">{intro}</p> : null}
      {contextNote ? (
        <Card className="border-dashed">
          <CardHeader className="space-y-1">
            <CardTitle className="text-sm">Vendor-specific output</CardTitle>
            <CardDescription>{contextNote}</CardDescription>
          </CardHeader>
        </Card>
      ) : null}
      <div className="flex items-center justify-between gap-3">
        <div className="text-xs text-muted-foreground">
          Reset to the original code template, or keep an inherited value and only override the prompts that need to change.
        </div>
        <Button onClick={handleSave} disabled={!dirty || saving}>
          {saving ? "Saving..." : "Save prompts"}
        </Button>
      </div>
      <div className="space-y-4">
        {templates.map((template) => (
          <Card key={template.key}>
            <CardHeader className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <CardTitle className="text-base">{template.title}</CardTitle>
                <Badge variant="outline" className="font-mono text-[11px]">
                  {template.key}
                </Badge>
                <Badge variant="secondary" className="text-[11px]">
                  effective: {template.effective_source}
                </Badge>
                {mode === "agent" ? (
                  <Badge variant="secondary" className="text-[11px]">
                    base: {template.base_source}
                  </Badge>
                ) : null}
              </div>
              <CardDescription>{template.description}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <Textarea
                value={drafts[template.key] ?? ""}
                onChange={(e) => setDrafts((current) => ({ ...current, [template.key]: e.target.value }))}
                className="min-h-40 font-mono text-sm leading-6"
              />
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    setDrafts((current) => ({
                      ...current,
                      [template.key]: template.default_template,
                    }))
                  }
                >
                  Reset to code default
                </Button>
                {mode === "agent" ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() =>
                      setDrafts((current) => ({
                        ...current,
                        [template.key]: template.base_template,
                      }))
                    }
                  >
                    Use inherited value
                  </Button>
                ) : null}
              </div>
              <div className="space-y-2">
                <div className="text-xs font-medium text-muted-foreground">Supported variables</div>
                <div className="flex flex-wrap gap-2">
                  {template.supported_variables.map((variable) => (
                    <Badge key={variable} variant="secondary" className="font-mono">
                      {`{{${variable}}}`}
                    </Badge>
                  ))}
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
