"use client";

import { useMemo, useState } from "react";
import { ChevronRight, Plus, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import {
  projectAgentListOptions,
  useAddProjectAgent,
  useRemoveProjectAgent,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "../../issues/components/pickers/property-picker";

export function ProjectAgentsSection({
  projectId,
  agents,
  canManage,
}: {
  projectId: string;
  agents: Agent[];
  canManage: boolean;
}) {
  const wsId = useWorkspaceId();
  const { data: assigned = [] } = useQuery(projectAgentListOptions(wsId, projectId));
  const addAgent = useAddProjectAgent(wsId, projectId);
  const removeAgent = useRemoveProjectAgent(wsId, projectId);
  const [open, setOpen] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const assignedIds = useMemo(() => new Set(assigned.map((a) => a.id)), [assigned]);
  const query = filter.trim().toLowerCase();
  const candidates = useMemo(
    () =>
      agents.filter(
        (a) =>
          !a.archived_at &&
          !assignedIds.has(a.id) &&
          (query === "" ||
            a.name.toLowerCase().includes(query) ||
            matchesPinyin(a.name, query)),
      ),
    [assignedIds, agents, query],
  );

  const handleAdd = async (agentId: string) => {
    try {
      await addAgent.mutateAsync(agentId);
      toast.success("Agent assigned to project");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to assign agent");
    }
  };

  const handleRemove = async (agentId: string) => {
    try {
      await removeAgent.mutateAsync(agentId);
      toast.success("Agent unassigned from project");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to unassign agent");
    }
  };

  return (
    <div className="mt-4 pt-4 border-t border-border">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Project agents
        <ChevronRight className={`ml-auto !size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-2">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              {assigned.length === 0 ? "No agents assigned yet." : `${assigned.length} assigned agent${assigned.length === 1 ? "" : "s"}`}
            </p>
            {canManage && (
              <PropertyPicker
                open={pickerOpen}
                onOpenChange={(v) => {
                  setPickerOpen(v);
                  if (!v) setFilter("");
                }}
                searchable
                searchPlaceholder="Find workspace agent"
                onSearchChange={setFilter}
                width="w-64"
                trigger={
                  <span className="inline-flex cursor-pointer items-center gap-1 rounded-md border border-dashed px-2 py-1 text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground">
                    <Plus className="size-3" />
                    Assign
                  </span>
                }
              >
                {candidates.length === 0 ? (
                  <PickerEmpty />
                ) : (
                  candidates.map((agent) => (
                    <PickerItem
                      key={agent.id}
                      selected={false}
                      onClick={() => {
                        void handleAdd(agent.id);
                        setPickerOpen(false);
                      }}
                    >
                      <ActorAvatar actorType="agent" actorId={agent.id} size={18} />
                      <span className="truncate">{agent.name}</span>
                    </PickerItem>
                  ))
                )}
              </PropertyPicker>
            )}
          </div>

          {assigned.length === 0 ? (
            <p className="rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
              {canManage ? "Assign workspace agents to this project." : "No agents assigned to this project."}
            </p>
          ) : (
            <ul className="space-y-1">
              {assigned.map((agent) => {
                return (
                  <li
                    key={agent.id}
                    className="group flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-muted/50"
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <ActorAvatar actorType="agent" actorId={agent.id} size={20} />
                      <span className="min-w-0">
                        <span className="block truncate text-sm">{agent.name}</span>
                      </span>
                    </span>
                    {canManage && (
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        className="opacity-0 group-hover:opacity-100 hover:text-destructive focus:opacity-100 hover:bg-transparent"
                        onClick={() => void handleRemove(agent.id)}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
