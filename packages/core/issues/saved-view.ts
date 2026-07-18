import type {
  ActorFilterValue,
  CardProperties,
  GanttZoom,
  IssueDateFilter,
  IssueGrouping,
  IssueViewState,
  SortDirection,
  SortField,
  SwimlaneGrouping,
  ViewMode,
} from "./stores/view-store";
import type { IssuesScope } from "./stores/issues-scope-store";

export interface SavedIssueViewSnapshot {
  scope: IssuesScope;
  viewMode: ViewMode;
  grouping: IssueGrouping;
  statusFilters: IssueViewState["statusFilters"];
  priorityFilters: IssueViewState["priorityFilters"];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  dateFilter: IssueDateFilter | null;
  sortBy: SortField;
  sortDirection: SortDirection;
  cardProperties: CardProperties;
  showSubIssues: boolean;
  listCollapsedStatuses: IssueViewState["listCollapsedStatuses"];
  ganttZoom: GanttZoom;
  ganttShowCompleted: boolean;
  swimlaneGrouping: SwimlaneGrouping;
  swimlaneOrders: IssueViewState["swimlaneOrders"];
  collapsedSwimlanes: IssueViewState["collapsedSwimlanes"];
}

function cloneActorFilters(filters: ActorFilterValue[]): ActorFilterValue[] {
  return filters.map((filter) => ({ ...filter }));
}

function cloneStringMap(map: Record<SwimlaneGrouping, string[]>): Record<SwimlaneGrouping, string[]> {
  return {
    parent: [...map.parent],
    project: [...map.project],
    assignee: [...map.assignee],
  };
}

export function snapshotIssueViewState(state: IssueViewState, scope: IssuesScope): SavedIssueViewSnapshot {
  return {
    scope,
    viewMode: state.viewMode,
    grouping: state.grouping,
    statusFilters: [...state.statusFilters],
    priorityFilters: [...state.priorityFilters],
    assigneeFilters: cloneActorFilters(state.assigneeFilters),
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: cloneActorFilters(state.creatorFilters),
    projectFilters: [...state.projectFilters],
    includeNoProject: state.includeNoProject,
    labelFilters: [...state.labelFilters],
    dateFilter: state.dateFilter ? { ...state.dateFilter } : null,
    sortBy: state.sortBy,
    sortDirection: state.sortDirection,
    cardProperties: { ...state.cardProperties },
    showSubIssues: state.showSubIssues,
    listCollapsedStatuses: [...state.listCollapsedStatuses],
    ganttZoom: state.ganttZoom,
    ganttShowCompleted: state.ganttShowCompleted,
    swimlaneGrouping: state.swimlaneGrouping,
    swimlaneOrders: cloneStringMap(state.swimlaneOrders),
    collapsedSwimlanes: cloneStringMap(state.collapsedSwimlanes),
  };
}

export function restoreIssueViewSnapshot(snapshot: SavedIssueViewSnapshot): Partial<IssueViewState> {
  return {
    viewMode: snapshot.viewMode,
    grouping: snapshot.grouping,
    statusFilters: [...snapshot.statusFilters],
    priorityFilters: [...snapshot.priorityFilters],
    assigneeFilters: cloneActorFilters(snapshot.assigneeFilters),
    includeNoAssignee: snapshot.includeNoAssignee,
    creatorFilters: cloneActorFilters(snapshot.creatorFilters),
    projectFilters: [...snapshot.projectFilters],
    includeNoProject: snapshot.includeNoProject,
    labelFilters: [...snapshot.labelFilters],
    dateFilter: snapshot.dateFilter ? { ...snapshot.dateFilter } : null,
    sortBy: snapshot.sortBy,
    sortDirection: snapshot.sortDirection,
    cardProperties: { ...snapshot.cardProperties },
    showSubIssues: snapshot.showSubIssues,
    listCollapsedStatuses: [...snapshot.listCollapsedStatuses],
    ganttZoom: snapshot.ganttZoom,
    ganttShowCompleted: snapshot.ganttShowCompleted,
    swimlaneGrouping: snapshot.swimlaneGrouping,
    swimlaneOrders: cloneStringMap(snapshot.swimlaneOrders),
    collapsedSwimlanes: cloneStringMap(snapshot.collapsedSwimlanes),
    agentRunningFilter: false,
  };
}
