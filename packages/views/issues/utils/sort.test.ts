import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { sortIssues } from "./sort";

function makeIssue(id: string, overrides: Partial<Issue>): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: Number(id.replace(/\D/g, "")) || 0,
    identifier: id.toUpperCase(),
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    project_id: null,
    stage: null,
    metadata: {},
    ...overrides,
  };
}

describe("sortIssues", () => {
  it("sorts by updated_at", () => {
    const issues = [
      makeIssue("issue-1", { updated_at: "2026-01-03T00:00:00Z" }),
      makeIssue("issue-2", { updated_at: "2026-01-01T00:00:00Z" }),
      makeIssue("issue-3", { updated_at: "2026-01-02T00:00:00Z" }),
    ];

    expect(sortIssues(issues, "updated_at", "asc").map((issue) => issue.id)).toEqual([
      "issue-2",
      "issue-3",
      "issue-1",
    ]);
    expect(sortIssues(issues, "updated_at", "desc").map((issue) => issue.id)).toEqual([
      "issue-1",
      "issue-3",
      "issue-2",
    ]);
  });
});
