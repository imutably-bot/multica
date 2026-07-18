/**
 * Mirrors the status / priority / date slice of `filterIssues()` at
 * packages/views/issues/utils/filter.ts. Same predicate, same
 * "empty array = show all" semantics — required by the same-N parity rule
 * in apps/mobile/CLAUDE.md.
 *
 * Mobile still defers assignee / project / label filters; the shared
 * date range now matches the web filter menu for created_at / updated_at.
 */
import { dateOnlyToUTCDate } from "@multica/core/issues/date";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import type { IssueDateFilter } from "@/data/stores/issue-date-filter";

function issueMatchesDateFilter(
  issue: Issue,
  filter: IssueDateFilter,
): boolean {
  const issueDate = dateOnlyToUTCDate(issue[filter.field]);
  const from = dateOnlyToUTCDate(filter.from);
  const to = dateOnlyToUTCDate(filter.to);
  if (!issueDate || !from || !to) return false;
  const lower = from.getTime() <= to.getTime() ? from : to;
  const upper = from.getTime() <= to.getTime() ? to : from;
  const time = issueDate.getTime();
  return time >= lower.getTime() && time <= upper.getTime();
}

export function filterIssues(
  issues: Issue[],
  statusFilters: IssueStatus[],
  priorityFilters: IssuePriority[],
  dateFilter: IssueDateFilter | null = null,
): Issue[] {
  return issues.filter((issue) => {
    if (
      statusFilters.length > 0 &&
      !statusFilters.includes(issue.status)
    ) {
      return false;
    }
    if (
      priorityFilters.length > 0 &&
      !priorityFilters.includes(issue.priority)
    ) {
      return false;
    }
    if (dateFilter && !issueMatchesDateFilter(issue, dateFilter)) {
      return false;
    }
    return true;
  });
}
