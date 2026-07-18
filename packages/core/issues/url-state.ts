import type {
  ActorFilterValue,
  IssueDateFilter,
  IssueViewState,
  SortDirection,
  SortField,
} from "./stores/view-store";
import type { IssuesScope } from "./stores/issues-scope-store";

export interface IssueFilterUrlState {
  scope: IssuesScope;
  statusFilters: string[];
  priorityFilters: string[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  dateFilter: IssueDateFilter | null;
}

export interface IssueViewUrlState extends IssueFilterUrlState {
  sortBy: SortField;
  sortDirection: SortDirection;
}

export interface IssueViewUrlStateInput {
  scope: IssuesScope;
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
}

const URL_KEYS = {
  scope: "scope",
  status: "status",
  priority: "priority",
  assignee: "assignee",
  includeNoAssignee: "noAssignee",
  creator: "creator",
  project: "project",
  includeNoProject: "noProject",
  label: "label",
  dateField: "dateField",
  dateFrom: "dateFrom",
  dateTo: "dateTo",
} as const;

const DEFAULT_FILTER_URL_STATE: IssueFilterUrlState = {
  scope: "all",
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  dateFilter: null,
};

function splitPair(value: string): ActorFilterValue | null {
  const idx = value.indexOf(":");
  if (idx <= 0 || idx >= value.length - 1) return null;
  const type = value.slice(0, idx);
  const id = value.slice(idx + 1);
  if (type !== "member" && type !== "agent" && type !== "squad") return null;
  return { type, id };
}

function isIssuesScope(value: string): value is IssuesScope {
  return value === "all" || value === "members" || value === "agents";
}

function sortedUnique(values: string[]): string[] {
  return [...new Set(values)].sort((a, b) => a.localeCompare(b));
}

function sortActors(values: ActorFilterValue[]): ActorFilterValue[] {
  return [...values]
    .map((value) => ({ ...value }))
    .sort((a, b) => `${a.type}:${a.id}`.localeCompare(`${b.type}:${b.id}`));
}

export function normalizeIssueFilterUrlState(state: IssueFilterUrlState): IssueFilterUrlState {
  return {
    scope: state.scope,
    statusFilters: sortedUnique(state.statusFilters),
    priorityFilters: sortedUnique(state.priorityFilters),
    assigneeFilters: sortActors(state.assigneeFilters),
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: sortActors(state.creatorFilters),
    projectFilters: sortedUnique(state.projectFilters),
    includeNoProject: state.includeNoProject,
    labelFilters: sortedUnique(state.labelFilters),
    dateFilter: state.dateFilter
      ? {
          field: state.dateFilter.field,
          from: state.dateFilter.from,
          to: state.dateFilter.to,
        }
      : null,
  };
}

export function hasIssueFilterUrlState(searchParams: URLSearchParams): boolean {
  for (const key of [
    URL_KEYS.scope,
    URL_KEYS.status,
    URL_KEYS.priority,
    URL_KEYS.assignee,
    URL_KEYS.includeNoAssignee,
    URL_KEYS.creator,
    URL_KEYS.project,
    URL_KEYS.includeNoProject,
    URL_KEYS.label,
    URL_KEYS.dateField,
    URL_KEYS.dateFrom,
    URL_KEYS.dateTo,
  ]) {
    if (searchParams.has(key)) return true;
  }
  return false;
}

export function readIssueFilterUrlState(searchParams: URLSearchParams): IssueFilterUrlState {
  const scopeParam = searchParams.get(URL_KEYS.scope);
  const scope = scopeParam && isIssuesScope(scopeParam) ? scopeParam : "all";
  const state: IssueFilterUrlState = {
    ...DEFAULT_FILTER_URL_STATE,
    scope,
    statusFilters: sortedUnique(searchParams.getAll(URL_KEYS.status)),
    priorityFilters: sortedUnique(searchParams.getAll(URL_KEYS.priority)),
    assigneeFilters: searchParams
      .getAll(URL_KEYS.assignee)
      .map(splitPair)
      .filter((value): value is ActorFilterValue => !!value),
    includeNoAssignee: searchParams.get(URL_KEYS.includeNoAssignee) === "1",
    creatorFilters: searchParams
      .getAll(URL_KEYS.creator)
      .map(splitPair)
      .filter((value): value is ActorFilterValue => !!value),
    projectFilters: sortedUnique(searchParams.getAll(URL_KEYS.project)),
    includeNoProject: searchParams.get(URL_KEYS.includeNoProject) === "1",
    labelFilters: sortedUnique(searchParams.getAll(URL_KEYS.label)),
    dateFilter:
      searchParams.get(URL_KEYS.dateField) &&
      searchParams.get(URL_KEYS.dateFrom) &&
      searchParams.get(URL_KEYS.dateTo)
        ? {
            field: searchParams.get(URL_KEYS.dateField) === "updated_at" ? "updated_at" : "created_at",
            from: searchParams.get(URL_KEYS.dateFrom)!,
            to: searchParams.get(URL_KEYS.dateTo)!,
          }
        : null,
  };

  return normalizeIssueFilterUrlState(state);
}

export function issueFilterUrlStateEquals(a: IssueFilterUrlState, b: IssueFilterUrlState): boolean {
  const left = normalizeIssueFilterUrlState(a);
  const right = normalizeIssueFilterUrlState(b);
  return (
    left.scope === right.scope &&
    left.includeNoAssignee === right.includeNoAssignee &&
    left.includeNoProject === right.includeNoProject &&
    JSON.stringify(left.statusFilters) === JSON.stringify(right.statusFilters) &&
    JSON.stringify(left.priorityFilters) === JSON.stringify(right.priorityFilters) &&
    JSON.stringify(left.assigneeFilters) === JSON.stringify(right.assigneeFilters) &&
    JSON.stringify(left.creatorFilters) === JSON.stringify(right.creatorFilters) &&
    JSON.stringify(left.projectFilters) === JSON.stringify(right.projectFilters) &&
    JSON.stringify(left.labelFilters) === JSON.stringify(right.labelFilters) &&
    JSON.stringify(left.dateFilter) === JSON.stringify(right.dateFilter)
  );
}

export function issueViewUrlStateFromInput(input: IssueViewUrlStateInput): IssueViewUrlState {
  const normalized = normalizeIssueFilterUrlState({
    scope: input.scope,
    statusFilters: input.statusFilters,
    priorityFilters: input.priorityFilters,
    assigneeFilters: input.assigneeFilters,
    includeNoAssignee: input.includeNoAssignee,
    creatorFilters: input.creatorFilters,
    projectFilters: input.projectFilters,
    includeNoProject: input.includeNoProject,
    labelFilters: input.labelFilters,
    dateFilter: input.dateFilter,
  });

  return {
    ...normalized,
    sortBy: input.sortBy,
    sortDirection: input.sortDirection,
  };
}

export function applyIssueFilterUrlState(
  baseSearchParams: URLSearchParams,
  state: IssueFilterUrlState,
): URLSearchParams {
  const next = new URLSearchParams(baseSearchParams.toString());
  for (const key of [
    URL_KEYS.scope,
    URL_KEYS.status,
    URL_KEYS.priority,
    URL_KEYS.assignee,
    URL_KEYS.includeNoAssignee,
    URL_KEYS.creator,
    URL_KEYS.project,
    URL_KEYS.includeNoProject,
    URL_KEYS.label,
    URL_KEYS.dateField,
    URL_KEYS.dateFrom,
    URL_KEYS.dateTo,
  ]) {
    next.delete(key);
  }

  const normalized = normalizeIssueFilterUrlState(state);
  if (normalized.scope !== "all") next.set(URL_KEYS.scope, normalized.scope);
  for (const value of normalized.statusFilters) next.append(URL_KEYS.status, value);
  for (const value of normalized.priorityFilters) next.append(URL_KEYS.priority, value);
  for (const value of normalized.assigneeFilters) next.append(URL_KEYS.assignee, `${value.type}:${value.id}`);
  if (normalized.includeNoAssignee) next.set(URL_KEYS.includeNoAssignee, "1");
  for (const value of normalized.creatorFilters) next.append(URL_KEYS.creator, `${value.type}:${value.id}`);
  for (const value of normalized.projectFilters) next.append(URL_KEYS.project, value);
  if (normalized.includeNoProject) next.set(URL_KEYS.includeNoProject, "1");
  for (const value of normalized.labelFilters) next.append(URL_KEYS.label, value);
  if (normalized.dateFilter) {
    next.set(URL_KEYS.dateField, normalized.dateFilter.field);
    next.set(URL_KEYS.dateFrom, normalized.dateFilter.from);
    next.set(URL_KEYS.dateTo, normalized.dateFilter.to);
  }

  return next;
}

export function getDefaultIssueFilterUrlState(): IssueFilterUrlState {
  return normalizeIssueFilterUrlState(DEFAULT_FILTER_URL_STATE);
}
