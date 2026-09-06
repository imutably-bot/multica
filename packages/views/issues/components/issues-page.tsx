"use client";

import { useEffect, useMemo, useRef } from "react";
import { ListTodo } from "lucide-react";
import type { Issue } from "@multica/core/types";
import { useIssuesScopeStore } from "@multica/core/issues/stores/issues-scope-store";
import { useIssueSavedViewsStore, restoreSavedIssueView, type SavedIssueView } from "@multica/core/issues/stores";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import type { IssueViewState } from "@multica/core/issues/stores/view-store";
import {
  applyIssueFilterUrlState,
  hasIssueFilterUrlState,
  issueFilterUrlStateEquals,
  normalizeIssueFilterUrlState,
  readIssueFilterUrlState,
  type IssueFilterUrlState,
} from "@multica/core/issues/url-state";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";

function issueViewStateToFilterUrlState(
  scope: string,
  viewState: Partial<IssueViewState>,
): IssueFilterUrlState {
  return normalizeIssueFilterUrlState({
    scope: scope as IssueFilterUrlState["scope"],
    statusFilters: viewState.statusFilters ?? [],
    priorityFilters: viewState.priorityFilters ?? [],
    assigneeFilters: viewState.assigneeFilters ?? [],
    includeNoAssignee: viewState.includeNoAssignee ?? false,
    creatorFilters: viewState.creatorFilters ?? [],
    projectFilters: viewState.projectFilters ?? [],
    includeNoProject: viewState.includeNoProject ?? false,
    excludeProjectFilters: viewState.excludeProjectFilters ?? [],
    labelFilters: viewState.labelFilters ?? [],
    dateFilter: viewState.dateFilter ?? null,
  });
}

function applyUrlFilterStateToViewStore(
  urlState: IssueFilterUrlState,
  viewStoreApi: ReturnType<typeof useViewStoreApi>,
) {
  viewStoreApi.setState({
    statusFilters: urlState.statusFilters as IssueViewState["statusFilters"],
    priorityFilters: urlState.priorityFilters as IssueViewState["priorityFilters"],
    assigneeFilters: urlState.assigneeFilters,
    includeNoAssignee: urlState.includeNoAssignee,
    creatorFilters: urlState.creatorFilters,
    projectFilters: urlState.projectFilters,
    includeNoProject: urlState.includeNoProject,
    excludeProjectFilters: urlState.excludeProjectFilters,
    labelFilters: urlState.labelFilters,
    dateFilter: urlState.dateFilter,
  });
}

function IssuesSurfaceHeader({
  issues,
  isRefreshing,
}: {
  issues: Issue[];
  isRefreshing: boolean;
}) {
  const navigation = useNavigation();
  const { searchParams, pathname } = navigation;
  const searchString = searchParams.toString();
  const setScope = useIssuesScopeStore((s) => s.setScope);
  const scope = useIssuesScopeStore((s) => s.scope);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const excludeProjectFilters = useViewStore((s) => s.excludeProjectFilters);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);
  const viewStoreApi = useViewStoreApi();
  const savedViews = useIssueSavedViewsStore((s) => s.views);
  const activeSavedView = useMemo<SavedIssueView | null>(() => {
    const viewId = searchParams.get("view");
    if (!viewId) return null;
    return savedViews.find((view) => view.id === viewId) ?? null;
  }, [savedViews, searchParams]);
  const currentFilterState = useMemo(
    () =>
      normalizeIssueFilterUrlState({
        scope,
        statusFilters,
        priorityFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        excludeProjectFilters,
        labelFilters,
        dateFilter,
      }),
    [
      assigneeFilters,
      creatorFilters,
      dateFilter,
      includeNoAssignee,
      includeNoProject,
      excludeProjectFilters,
      labelFilters,
      priorityFilters,
      projectFilters,
      scope,
      statusFilters,
    ],
  );
  const urlFilterState = useMemo(
    () => (hasIssueFilterUrlState(searchParams) ? readIssueFilterUrlState(searchParams) : null),
    [searchParams],
  );
  const initialSyncCompleteRef = useRef(false);
  const lastSyncedSearchStringRef = useRef<string | null>(null);

  useEffect(() => {
    if (!initialSyncCompleteRef.current) {
      if (urlFilterState) {
        if (!issueFilterUrlStateEquals(currentFilterState, urlFilterState)) {
          setScope(urlFilterState.scope);
          applyUrlFilterStateToViewStore(urlFilterState, viewStoreApi);
          initialSyncCompleteRef.current = true;
          lastSyncedSearchStringRef.current = searchString;
          return;
        }
      } else if (activeSavedView) {
        const restored = restoreSavedIssueView(activeSavedView);
        const restoredFilterState = issueViewStateToFilterUrlState(restored.scope, restored.viewState);
        if (!issueFilterUrlStateEquals(currentFilterState, restoredFilterState) || scope !== restored.scope) {
          setScope(restored.scope);
          viewStoreApi.setState(restored.viewState as IssueViewState);
          initialSyncCompleteRef.current = true;
          lastSyncedSearchStringRef.current = searchString;
          return;
        }
      }

      initialSyncCompleteRef.current = true;
    }

    const nextSearchParams = applyIssueFilterUrlState(searchParams, currentFilterState);
    const nextSearchString = nextSearchParams.toString();
    if (nextSearchString !== searchString && nextSearchString !== lastSyncedSearchStringRef.current) {
      lastSyncedSearchStringRef.current = nextSearchString;
      navigation.replace(nextSearchString ? `${pathname}?${nextSearchString}` : pathname);
    }
  }, [
    activeSavedView,
    currentFilterState,
    navigation,
    pathname,
    scope,
    searchParams,
    searchString,
    setScope,
    urlFilterState,
    viewStoreApi,
  ]);

  return (
    <IssuesHeader
      scopedIssues={issues}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      activeSavedView={activeSavedView}
    />
  );
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScopeStore((s) => s.scope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            isRefreshing={controller.isRefreshing}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_hint)}</p>
          </div>
        )}
      />
    </div>
  );
}
