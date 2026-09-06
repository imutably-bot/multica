"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import type { SavedIssueViewSnapshot } from "../saved-view";
import { snapshotIssueViewState, restoreIssueViewSnapshot } from "../saved-view";
import type { IssueViewState } from "./view-store";
import type { IssuesScope } from "./issues-scope-store";

export interface SavedIssueView {
  id: string;
  name: string;
  snapshot: SavedIssueViewSnapshot;
  created_at: string;
  updated_at: string;
}

interface SavedIssueViewStore {
  views: SavedIssueView[];
  saveView: (name: string, state: IssueViewState, scope: SavedIssueViewSnapshot["scope"], id?: string) => SavedIssueView;
  deleteView: (id: string) => void;
}

export interface RestoredSavedIssueView {
  scope: IssuesScope;
  viewState: Partial<IssueViewState>;
}

function nowIso() {
  return new Date().toISOString();
}

function newId() {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function sortViews(views: SavedIssueView[]): SavedIssueView[] {
  return [...views].sort((a, b) => b.updated_at.localeCompare(a.updated_at));
}

export const useIssueSavedViewsStore = create<SavedIssueViewStore>()(
  persist(
    (set, _get) => ({
      views: [],
      saveView: (name, state, scope, id) => {
        const timestamp = nowIso();
        const view: SavedIssueView = {
          id: id ?? newId(),
          name: name.trim() || "Custom view",
          snapshot: snapshotIssueViewState(state, scope),
          created_at: timestamp,
          updated_at: timestamp,
        };
        set((current) => {
          const others = current.views.filter((item) => item.id !== view.id);
          return { views: sortViews([view, ...others]) };
        });
        return view;
      },
      deleteView: (id) =>
        set((current) => ({ views: current.views.filter((view) => view.id !== id) })),
    }),
    {
      name: "multica_issue_saved_views",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ views: state.views }),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<SavedIssueViewStore>;
        return {
          ...current,
          views: sortViews(p.views ?? []),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueSavedViewsStore.persist.rehydrate());

export function restoreSavedIssueView(view: SavedIssueView): RestoredSavedIssueView {
  return {
    scope: view.snapshot.scope,
    viewState: restoreIssueViewSnapshot(view.snapshot),
  };
}
