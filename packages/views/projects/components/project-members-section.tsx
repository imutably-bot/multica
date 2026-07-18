"use client";

import { useMemo, useState } from "react";
import { ChevronRight, Plus, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MemberWithUser } from "@multica/core/types";
import {
  projectMembersOptions,
  useAddProjectMember,
  useRemoveProjectMember,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "../../issues/components/pickers/property-picker";
import { useT } from "../../i18n";

export function ProjectMembersSection({
  projectId,
  members,
  canManage,
}: {
  projectId: string;
  members: MemberWithUser[];
  canManage: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { data: response } = useQuery(projectMembersOptions(wsId, projectId));
  const addMember = useAddProjectMember(wsId, projectId);
  const removeMember = useRemoveProjectMember(wsId, projectId);
  const [open, setOpen] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const assigned = response?.members ?? [];
  const assignedIds = useMemo(() => new Set(assigned.map((m) => m.user_id)), [assigned]);
  const memberById = useMemo(() => new Map(members.map((m) => [m.user_id, m])), [members]);
  const query = filter.trim().toLowerCase();
  const candidates = useMemo(
    () =>
      members.filter(
        (m) =>
          !assignedIds.has(m.user_id) &&
          (query === "" ||
            m.name.toLowerCase().includes(query) ||
            matchesPinyin(m.name, query)),
      ),
    [assignedIds, members, query],
  );

  const handleAdd = async (userId: string) => {
    try {
      await addMember.mutateAsync({ user_id: userId });
      toast.success("Member added to project");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to add project member");
    }
  };

  const handleRemove = async (userId: string) => {
    try {
      await removeMember.mutateAsync(userId);
      toast.success("Member removed from project");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to remove project member");
    }
  };

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Project members
        <ChevronRight className={`ml-auto !size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-2">
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              {assigned.length === 0 ? "No members assigned yet." : `${assigned.length} assigned member${assigned.length === 1 ? "" : "s"}`}
            </p>
            {canManage && (
              <PropertyPicker
                open={pickerOpen}
                onOpenChange={(v) => {
                  setPickerOpen(v);
                  if (!v) setFilter("");
                }}
                searchable
                searchPlaceholder="Find workspace member"
                onSearchChange={setFilter}
                width="w-64"
                trigger={
                  <span className="inline-flex cursor-pointer items-center gap-1 rounded-md border border-dashed px-2 py-1 text-xs text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground">
                    <Plus className="size-3" />
                    Add
                  </span>
                }
              >
                {candidates.length === 0 ? (
                  <PickerEmpty />
                ) : (
                  candidates.map((member) => (
                    <PickerItem
                      key={member.user_id}
                      selected={false}
                      onClick={() => {
                        void handleAdd(member.user_id);
                        setPickerOpen(false);
                      }}
                    >
                      <ActorAvatar actorType="member" actorId={member.user_id} size={18} />
                      <span className="truncate">{member.name}</span>
                    </PickerItem>
                  ))
                )}
              </PropertyPicker>
            )}
          </div>

          {assigned.length === 0 ? (
            <p className="rounded-md border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
              {canManage ? "Add workspace members to assign them to this project." : "No members assigned to this project."}
            </p>
          ) : (
            <ul className="space-y-1">
              {assigned.map((member) => {
                const actor = memberById.get(member.user_id);
                const name = actor?.name ?? member.user_id;
                const addedBy = memberById.get(member.added_by)?.name;
                return (
                  <li
                    key={member.user_id}
                    className="flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-muted/50"
                  >
                    <span className="flex min-w-0 items-center gap-2">
                      <ActorAvatar actorType="member" actorId={member.user_id} size={20} />
                      <span className="min-w-0">
                        <span className="block truncate text-sm">{name}</span>
                        {addedBy && (
                          <span className="block text-[11px] text-muted-foreground">
                            added by {addedBy}
                          </span>
                        )}
                      </span>
                    </span>
                    {canManage && (
                      <button
                        type="button"
                        onClick={() => void handleRemove(member.user_id)}
                        disabled={removeMember.isPending}
                        className="cursor-pointer text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
                        aria-label="Remove project member"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}

          {!canManage && (
            <p className="text-xs text-muted-foreground">
              Workspace owners and admins manage project membership.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
