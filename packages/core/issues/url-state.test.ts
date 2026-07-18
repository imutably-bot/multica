import { describe, expect, it } from "vitest";
import {
  applyIssueFilterUrlState,
  issueFilterUrlStateEquals,
  readIssueFilterUrlState,
  type IssueFilterUrlState,
} from "./url-state";

describe("issue url state", () => {
  it("round-trips filter params and preserves unrelated query keys", () => {
    const state: IssueFilterUrlState = {
      scope: "agents",
      statusFilters: ["todo", "backlog"],
      priorityFilters: ["high"],
      assigneeFilters: [
        { type: "member", id: "m-2" },
        { type: "agent", id: "a-1" },
      ],
      includeNoAssignee: true,
      creatorFilters: [{ type: "squad", id: "s-1" }],
      projectFilters: ["proj-b", "proj-a"],
      includeNoProject: true,
      excludeProjectFilters: ["proj-c"],
      labelFilters: ["label-2", "label-1"],
      dateFilter: { field: "updated_at", from: "2026-01-01", to: "2026-01-31" },
    };

    const next = applyIssueFilterUrlState(new URLSearchParams("view=view-123&tab=board"), state);
    expect(next.toString()).toBe(
      [
        "view=view-123",
        "tab=board",
        "scope=agents",
        "status=backlog",
        "status=todo",
        "priority=high",
        "assignee=agent%3Aa-1",
        "assignee=member%3Am-2",
        "noAssignee=1",
        "creator=squad%3As-1",
        "project=proj-a",
        "project=proj-b",
        "noProject=1",
        "excludeProject=proj-c",
        "label=label-1",
        "label=label-2",
        "dateField=updated_at",
        "dateFrom=2026-01-01",
        "dateTo=2026-01-31",
      ].join("&"),
    );

    expect(issueFilterUrlStateEquals(readIssueFilterUrlState(next), state)).toBe(true);
  });

  it("treats invalid scope values as the default", () => {
    const parsed = readIssueFilterUrlState(new URLSearchParams("scope=bad-value"));
    expect(parsed.scope).toBe("all");
  });

  it("compares normalized filter state", () => {
    const a: IssueFilterUrlState = {
      scope: "all",
      statusFilters: ["todo", "backlog"],
      priorityFilters: [],
      assigneeFilters: [],
      includeNoAssignee: false,
      creatorFilters: [],
      projectFilters: [],
      includeNoProject: false,
      excludeProjectFilters: [],
      labelFilters: [],
      dateFilter: null,
    };
    const b: IssueFilterUrlState = {
      ...a,
      statusFilters: ["backlog", "todo"],
    };

    expect(issueFilterUrlStateEquals(a, b)).toBe(true);
  });

  it("round-trips relative date presets", () => {
    const next = readIssueFilterUrlState(
      new URLSearchParams("dateField=created_at&datePreset=last_days&dateDays=7"),
    );
    expect(next.dateFilter?.preset).toBe("last_days");
    expect(next.dateFilter?.days).toBe(7);
    expect(next.dateFilter?.field).toBe("created_at");
  });
});
