package execenv

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/prompttmpl"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
)

func runtimeTemplate(templates map[string]string, key string, values map[string]string) string {
	template, ok := templates[key]
	if !ok {
		template = prompttmpl.DefaultTemplate(key)
	}
	return prompttmpl.Render(template, values)
}

// This file holds the slim runtime brief — the post-MUL-3560 path that
// `buildMetaSkillContent` routes to when the `runtime_brief_slim` feature
// flag is enabled. The legacy path lives untouched in runtime_config.go.
//
// Layout:
//
//   - buildMetaSkillContentSlim is the entry point.
//   - It calls classifyTask (runtime_config_kind.go) to pick one of five
//     task kinds, then composes the brief from the per-section writers
//     below.
//   - Each section is its own writer so the matrix of "which kind gets
//     which section" lives at a single dispatch site.
//
// The slim path applies two orthogonal optimisations:
//
//  1. Section gating per task kind — quick-create / chat / autopilot
//     skip sections they have no use for (Mentions, Comment Formatting,
//     Issue Metadata, Sub-issue, ...).
//  2. Per-section prose compression — Available Commands, Issue
//     Metadata, Mentions, Sub-issue Creation, Comment Formatting,
//     Always Use CLI, Background Task Safety, Task Initiator,
//     Repositories, Output are all tightened. Every test-asserted phrase
//     stays.
//
// Background Task Safety still lives in runtime_config.go because the
// helper there (`writeBackgroundTaskSafetyInstructions`) is the legacy
// implementation. The slim path emits its own compressed version via
// `writeBackgroundTaskSafetySlim` below.

// writeHeader emits the brief's leading title and one-line elevator pitch.
func writeHeader(b *strings.Builder, templates map[string]string) {
	b.WriteString(runtimeTemplate(templates, prompttmpl.RuntimeHeaderKey, nil))
	b.WriteString("\n")
}

// writeBackgroundTaskSafetySlim is the slim analogue of
// writeBackgroundTaskSafetyInstructions (legacy). Drops the verbose
// preamble and keeps the three behaviour pins (the same ones tests
// assert): "Do NOT end your turn while background tasks",
// "wait for a future notification/reminder", "run the work synchronously
// instead".
func writeBackgroundTaskSafetySlim(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Background Task Safety\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.BackgroundSafetyKey, nil))
	b.WriteString("\n")
}

// writeAgentIdentity emits the Agent Identity heading and (optionally) the
// agent's instructions body.
func writeAgentIdentity(b *strings.Builder, ctx TaskContextForEnv) {
	if ctx.AgentName != "" || ctx.AgentID != "" {
		b.WriteString("## Agent Identity\n\n")
		agentNameBlock := ""
		if ctx.AgentName != "" {
			agentNameBlock = fmt.Sprintf("**You are: %s**", ctx.AgentName)
			if ctx.AgentID != "" {
				agentNameBlock += fmt.Sprintf(" (ID: `%s`)", ctx.AgentID)
			}
			agentNameBlock += "\n\n"
		}
		agentInstructionsBlock := ""
		if ctx.AgentInstructions != "" {
			agentInstructionsBlock = ctx.AgentInstructions + "\n\n"
		}
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.AgentIdentityKey, map[string]string{
			"agent_name_block":         agentNameBlock,
			"agent_instructions_block": agentInstructionsBlock,
		}))
		return
	}
	if ctx.AgentInstructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.AgentIdentityKey, map[string]string{
			"agent_name_block":         "",
			"agent_instructions_block": ctx.AgentInstructions + "\n\n",
		}))
	}
}

// writeRequestingUser emits the Requesting User block when the runtime
// owner's profile description is non-empty. Sanitisation rules match the
// legacy implementation; see runtime_config.go for the rationale.
func writeRequestingUser(b *strings.Builder, ctx TaskContextForEnv) {
	if strings.TrimSpace(ctx.RequestingUserProfileDescription) == "" {
		return
	}
	b.WriteString("## Requesting User\n\n")
	safeName := sanitizeNameForBriefMarkdown(ctx.RequestingUserName)
	desc := strings.ReplaceAll(ctx.RequestingUserProfileDescription, "\r\n", "\n")
	desc = strings.ReplaceAll(desc, "\r", "\n")
	desc = strings.TrimRight(desc, "\n")
	var quoted strings.Builder
	for _, line := range strings.Split(desc, "\n") {
		quoted.WriteString("> ")
		quoted.WriteString(line)
		quoted.WriteString("\n")
	}
	intro := ""
	if safeName != "" {
		intro = fmt.Sprintf("You are working on behalf of **%s**. They describe themselves as:\n\n", safeName)
	} else {
		intro = "You are working on behalf of the following user. They describe themselves as:\n\n"
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.RequestingUserKey, map[string]string{
		"requesting_user_intro":             intro,
		"requesting_user_description_block": quoted.String(),
	}))
	b.WriteString("\n")
}

// writeTaskInitiator emits the Task Initiator block when an initiator name
// resolves. Compressed from two paragraphs to one in the slim path; both
// MUL-2645 test-pinned phrases ("apply any per-person privacy or access
// rules" and "credentials stay scoped to the runtime owner") are kept.
func writeTaskInitiator(b *strings.Builder, ctx TaskContextForEnv) {
	safeInitiator := sanitizeNameForBriefMarkdown(ctx.InitiatorName)
	if safeInitiator == "" {
		return
	}
	b.WriteString("## Task Initiator\n\n")
	identity := ""
	if ctx.InitiatorType == "agent" {
		identity = fmt.Sprintf("This task was initiated by **%s**, another agent in this workspace.\n\n", safeInitiator)
	} else if email := sanitizeEmailForBrief(ctx.InitiatorEmail); email != "" {
		identity = fmt.Sprintf("This task was initiated by **%s** (%s), a member of this workspace.\n\n", safeInitiator, email)
	} else {
		identity = fmt.Sprintf("This task was initiated by **%s**, a member of this workspace.\n\n", safeInitiator)
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.TaskInitiatorKey, map[string]string{
		"task_initiator_identity": identity,
	}))
	b.WriteString("\n")
}

func taskTypeLabel(ctx TaskContextForEnv) string {
	switch classifyTask(ctx) {
	case kindCommentTriggered:
		return "comment"
	case kindAssignmentTriggered:
		return "assignment"
	case kindAutopilotRunOnly:
		return "autopilot"
	case kindQuickCreate:
		return "quick_create"
	case kindChat:
		return "chat"
	default:
		return "unknown"
	}
}

func firstRepoURL(ctx TaskContextForEnv) string {
	if len(ctx.Repos) == 0 {
		return ""
	}
	return strings.TrimSpace(ctx.Repos[0].URL)
}

func renderWorkspaceInitPrompt(template string, ctx TaskContextForEnv) string {
	return strings.NewReplacer(
		"{{agent_name}}", sanitizeNameForBriefMarkdown(ctx.AgentName),
		"{{workspace_name}}", sanitizeNameForBriefMarkdown(ctx.WorkspaceName),
		"{{issue_id}}", sanitizeBriefCodeToken(ctx.IssueID),
		"{{chat_id}}", sanitizeBriefCodeToken(ctx.ChatSessionID),
		"{{user_name}}", func() string {
			if name := sanitizeNameForBriefMarkdown(ctx.InitiatorName); name != "" {
				return name
			}
			return sanitizeNameForBriefMarkdown(ctx.RequestingUserName)
		}(),
		"{{repo_url}}", sanitizeNameForBriefMarkdown(firstRepoURL(ctx)),
		"{{task_type}}", sanitizeBriefCodeToken(taskTypeLabel(ctx)),
	).Replace(template)
}

// writeWorkspaceInitPrompt emits the short workspace-level init prompt
// configured by the workspace owner. Trailing whitespace is stripped and the
// default prompt is rendered when the workspace setting is empty so old
// workspaces inherit the built-in copy without a migration.
func writeWorkspaceInitPrompt(b *strings.Builder, ctx TaskContextForEnv) {
	template := strings.TrimSpace(ctx.WorkspaceInitPrompt)
	if template == "" && ctx.PromptTemplates != nil {
		template = strings.TrimSpace(ctx.PromptTemplates[prompttmpl.WorkspaceInitKey])
	}
	if template == "" {
		template = prompttmpl.DefaultTemplate(prompttmpl.WorkspaceInitKey)
	}
	rendered := strings.TrimSpace(renderWorkspaceInitPrompt(template, ctx))
	if rendered == "" {
		return
	}
	b.WriteString("## Init Prompt\n\n")
	b.WriteString(rendered)
	b.WriteString("\n\n")
}

// writeWorkspaceContext emits the workspace-level system prompt configured
// by the workspace owner. Trailing whitespace is stripped.
func writeWorkspaceContext(b *strings.Builder, ctx TaskContextForEnv) {
	ctxText := strings.TrimRight(ctx.WorkspaceContext, " \t\r\n")
	if ctxText == "" {
		return
	}
	b.WriteString("## Workspace Context\n\n")
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.WorkspaceContextKey, map[string]string{
		"workspace_context": ctxText,
	}))
	b.WriteString("\n\n")
}

func writeConnectedApps(b *strings.Builder, ctx TaskContextForEnv) {
	if len(ctx.ConnectedApps) == 0 {
		return
	}
	var lines strings.Builder
	for _, app := range ctx.ConnectedApps {
		serverName := sanitizeBriefCodeToken(app.ServerName)
		toolkitSlug := sanitizeBriefCodeToken(app.ToolkitSlug)
		if serverName == "" || toolkitSlug == "" {
			continue
		}
		name := sanitizeNameForBriefMarkdown(app.ToolkitName)
		if name == "" {
			name = sanitizeNameForBriefMarkdown(runtimeapps.DisplayNameForToolkitSlug(toolkitSlug))
		}
		if name == "" {
			name = toolkitSlug
		}
		fmt.Fprintf(&lines, "- %s (`%s`) via MCP server `%s`\n", name, toolkitSlug, serverName)
	}
	if lines.Len() == 0 {
		return
	}
	b.WriteString("## Connected Apps\n\n")
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.ConnectedAppsKey, map[string]string{
		"connected_apps_list": lines.String(),
	}))
	b.WriteString("\n")
}

func sanitizeBriefCodeToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return ""
	}
	return s
}

// writeAvailableCommands emits the slim Available Commands section
// (~2.4k chars vs legacy ~4.4k). Every test-asserted substring is
// preserved: each `multica issue …` command name, all three `comment add`
// input modes, `--description-file <path>`, `--parent ""`, the
// `Next reply cursor` / `Next thread cursor` stderr labels, the three
// metadata discovery lines, the "core agent loop and common issue
// create/update tasks" intro phrase, and `multica issue comment add
// --help`.
//
// The fold-aware `--full` flag from MUL-3555 is documented inline on the
// comment-list bullet so the slim brief preserves the same agent
// behaviour as the legacy brief on that path.
func writeAvailableCommands(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Available Commands\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.AvailableCmdsKey, nil))
	b.WriteString("\n")
}

// writeAvailableCommandsQuickCreate emits a minimal Available Commands
// section for quick-create runs. Quick-create's hard guardrails forbid
// every CLI other than `multica issue create`, so listing more would just
// tempt the model to bend the guardrail.
func writeAvailableCommandsQuickCreate(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Available Commands\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.AvailableCmdsQCKey, nil))
	b.WriteString("\n")
}

// writeCommentFormatting emits the cross-platform file-first guardrail.
// Windows branch carries the `$OutputEncoding` rationale because Windows
// PowerShell silently drops non-ASCII through stdin.
func writeCommentFormatting(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Comment Formatting\n\n")
	if runtimeGOOS == "windows" {
		b.WriteString(runtimeTemplate(templates, prompttmpl.CommentFormatWinKey, nil))
		b.WriteString("\n")
		return
	}
	b.WriteString(runtimeTemplate(templates, prompttmpl.CommentFormatKey, nil))
	b.WriteString("\n")
}

// writeRepositories emits the Repositories section when at least one repo
// is configured. The closing paragraph from the legacy version is dropped
// (it re-stated the opening); intro is tightened into one line.
func writeRepositories(b *strings.Builder, ctx TaskContextForEnv) {
	if len(ctx.Repos) == 0 {
		return
	}
	b.WriteString("## Repositories\n\n")
	var repoList strings.Builder
	for _, repo := range ctx.Repos {
		if repo.Description != "" {
			fmt.Fprintf(&repoList, "- %s — %s\n", repo.URL, repo.Description)
		} else {
			fmt.Fprintf(&repoList, "- %s\n", repo.URL)
		}
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.RepositoriesKey, map[string]string{
		"repo_list": repoList.String(),
	}))
	b.WriteString("\n")
}

// writeProjectContext emits the Project Context section when the issue
// belongs to a project.
func writeProjectContext(b *strings.Builder, ctx TaskContextForEnv) {
	if ctx.ProjectID == "" && len(ctx.ProjectResources) == 0 {
		return
	}
	b.WriteString("## Project Context\n\n")
	projectTitleBlock := ""
	if ctx.ProjectTitle != "" {
		projectTitleBlock = fmt.Sprintf("This issue belongs to **%s**.\n\n", ctx.ProjectTitle)
	}
	projectDescriptionBlock := ""
	if desc := strings.TrimSpace(ctx.ProjectDescription); desc != "" {
		projectDescriptionBlock = "Project description — durable context the project owner set for every task in this project:\n\n" + desc + "\n\n"
	}
	projectResourcesIntroBlock := ""
	projectResourcesListBlock := ""
	projectResourcesOutroBlock := ""
	if len(ctx.ProjectResources) > 0 {
		projectResourcesIntroBlock = "Project resources (also written to `.multica/project/resources.json`):\n\n"
		var resourcesList strings.Builder
		for _, r := range ctx.ProjectResources {
			fmt.Fprintf(&resourcesList, "- %s\n", formatProjectResource(r))
		}
		projectResourcesListBlock = resourcesList.String()
		projectResourcesOutroBlock = "\nResources are pointers — open them only when relevant to the task. For `github_repo` resources, use `multica repo checkout <url>` to fetch the code. Add `--ref <branch-or-sha>` when a task or handoff names an exact revision.\n\n"
	} else {
		projectResourcesIntroBlock = "This project has no resources attached yet.\n\n"
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.ProjectContextKey, map[string]string{
		"project_title_block":           projectTitleBlock,
		"project_description_block":     projectDescriptionBlock,
		"project_resources_intro_block": projectResourcesIntroBlock,
		"project_resources_list_block":  projectResourcesListBlock,
		"project_resources_outro_block": projectResourcesOutroBlock,
	}))
	b.WriteString("\n")
}

// writeIssueMetadata emits the Issue Metadata discipline section
// (compressed). The dispatcher gates by kind.hasIssueContext(); this
// helper does not re-check.
func writeIssueMetadata(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Issue Metadata\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.IssueMetadataKey, nil))
	b.WriteString("\n")
}

// writeInstructionPrecedence emits the "Agent Identity wins over the
// assignment workflow below" guardrail. Caller gates on
// kind == kindAssignmentTriggered.
func writeInstructionPrecedence(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Instruction Precedence\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.InstructionPrecKey, nil))
	b.WriteString("\n")
}

// writeWorkflowHeader emits the unconditional `### Workflow` heading.
func writeWorkflowHeader(b *strings.Builder) {
	b.WriteString("### Workflow\n\n")
}

// writeWorkflowChat emits the chat-mode workflow.
func writeWorkflowChat(b *strings.Builder, templates map[string]string) {
	b.WriteString(runtimeTemplate(templates, prompttmpl.WorkflowChatKey, nil))
	b.WriteString("\n")
}

// writeWorkflowQuickCreate emits the quick-create workflow's hard
// guardrails.
func writeWorkflowQuickCreate(b *strings.Builder, templates map[string]string) {
	b.WriteString(runtimeTemplate(templates, prompttmpl.WorkflowQuickKey, nil))
	b.WriteString("\n")
}

// writeWorkflowAutopilot emits the autopilot run-only workflow.
func writeWorkflowAutopilot(b *strings.Builder, ctx TaskContextForEnv) {
	values := map[string]string{
		"autopilot_run_line": fmt.Sprintf("- Autopilot run ID: `%s`\n", ctx.AutopilotRunID),
	}
	if ctx.AutopilotID != "" {
		values["autopilot_id_line"] = fmt.Sprintf("- Autopilot ID: `%s`\n", ctx.AutopilotID)
	}
	if ctx.AutopilotTitle != "" {
		values["autopilot_title_line"] = fmt.Sprintf("- Autopilot title: %s\n", ctx.AutopilotTitle)
	}
	if ctx.AutopilotSource != "" {
		values["autopilot_source_line"] = fmt.Sprintf("- Trigger source: %s\n", ctx.AutopilotSource)
	}
	if ctx.AutopilotTriggerPayload != "" {
		values["autopilot_payload_block"] = fmt.Sprintf("- Trigger payload:\n\n```json\n%s\n```\n", ctx.AutopilotTriggerPayload)
	}
	if strings.TrimSpace(ctx.AutopilotDescription) != "" {
		values["autopilot_instructions_block"] = "\nAutopilot instructions:\n\n" + ctx.AutopilotDescription + "\n\n"
	}
	if ctx.AutopilotID != "" {
		values["autopilot_get_line"] = fmt.Sprintf("- Run `multica autopilot get %s --output json` if you need the full autopilot configuration\n", ctx.AutopilotID)
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.WorkflowAutopilotKey, values))
	b.WriteString("\n")
}

// writeWorkflowComment emits the comment-triggered workflow.
func writeWorkflowComment(b *strings.Builder, provider string, ctx TaskContextForEnv) {
	stepCommentRead := ""
	if hint := BuildNewCommentsHint(ctx.PromptTemplates, ctx.IssueID, ctx.TriggerCommentID, ctx.TriggerThreadID, ctx.NewCommentsSince, ctx.NewCommentCount); hint != "" {
		stepCommentRead = strings.TrimRight(hint, "\n")
	} else if ctx.PriorSessionResumed {
		stepCommentRead = strings.TrimRight(BuildResumedCommentsHint(ctx.PromptTemplates, ctx.IssueID, ctx.TriggerCommentID, ctx.TriggerThreadID), "\n")
	} else if cold := BuildColdCommentsHint(ctx.PromptTemplates, ctx.IssueID, ctx.TriggerCommentID, ctx.TriggerThreadID); cold != "" {
		stepCommentRead = strings.TrimRight(cold, "\n")
	} else {
		stepCommentRead = fmt.Sprintf("Catch up on comments — read with `multica issue comment list %s --recent 10 --output json` (resolved threads come back folded — `--full` to expand).", ctx.IssueID)
	}
	replyDecision := "If you produced actual work this turn (investigated, fixed, answered a real question), post the result via step 7 — that is a normal reply, not a noise comment. If the triggering comment was a pure acknowledgment / thanks / sign-off from another agent AND you produced no work this turn, do NOT post a reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is a valid and preferred way to end agent-to-agent conversations."
	squadLeaderRule := ""
	if ctx.IsSquadLeader {
		squadLeaderRule = fmt.Sprintf("   - **Squad leader rule:** If your evaluation outcome is `no_action`, call `multica squad activity %s no_action --reason \"...\"` and then EXIT IMMEDIATELY. DO NOT post any comment whose only purpose is to announce that you are taking no action, exiting silently, or acknowledging another agent. A comment like \"No action needed\" or \"Exiting silently\" is noise — the `squad activity` call already records your decision in the timeline.\n", ctx.IssueID)
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.WorkflowCommentKey, map[string]string{
		"step_issue_get":          fmt.Sprintf("Run `multica issue get %s --output json` to understand the issue context", ctx.IssueID),
		"step_metadata_list":      fmt.Sprintf("Run `multica issue metadata list %s --output json` to see what prior agents pinned — best-effort, empty `{}` and CLI failures are normal. See the `## Issue Metadata` section above for what to look for.", ctx.IssueID),
		"step_comment_read":       stepCommentRead,
		"step_trigger_comment":    fmt.Sprintf("Find the triggering comment (ID: `%s`) and understand what is being asked — do NOT confuse it with previous comments", ctx.TriggerCommentID),
		"step_reply_decision":     replyDecision,
		"squad_leader_rule_block": squadLeaderRule,
		"step_mentions":           "If a reply IS warranted: do any requested work first, then **decide whether to include any `@mention` link.** The default is NO mention. Only mention when you are escalating to a human owner who is not yet involved, delegating a concrete new sub-task to another agent for the first time, or the user explicitly asked you to loop someone in. Never @mention the agent you are replying to as a thank-you or sign-off.",
		"step_reply_post":         "**If you reply, post it as a comment — this step is mandatory when you reply.** Text in your terminal or run logs is NOT delivered to the user. " + strings.TrimRight(BuildCommentReplyInstructions(provider, ctx.PromptTemplates, ctx.IssueID, ctx.TriggerCommentID), "\n"),
		"step_metadata_exit":      "Before exiting: only if this run produced a fact that clears the high bar (important AND likely to be re-read by future runs on this same issue, e.g. a new PR URL or deploy URL), or you noticed a metadata key from entry that is now stale, pin or clear it via `multica issue metadata set`/`delete`. Most runs write nothing here — that is the expected outcome, not a gap. When in doubt, do not write. See the `## Issue Metadata` section above for the full bar.",
		"step_status_guardrail":   "Do NOT change the issue status unless the comment explicitly asks for it",
	}))
	b.WriteString("\n")
}

// writeWorkflowAssignment emits the assignment-triggered workflow.
func writeWorkflowAssignment(b *strings.Builder, ctx TaskContextForEnv) {
	finalCommentStep := ""
	if ctx.IsSquadLeader {
		finalCommentStep = fmt.Sprintf("**Post your final results as a comment** (unless your outcome is `no_action` — in that case, calling `multica squad activity %s no_action --reason \"...\"` alone is sufficient; you MUST exit without posting any comment. DO NOT post a comment announcing no_action or saying you are exiting silently): post it with `multica issue comment add %s` using the platform-correct non-inline mode from ## Comment Formatting (never inline `--content`). Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.", ctx.IssueID, ctx.IssueID)
	} else {
		finalCommentStep = fmt.Sprintf("**Post your final results as a comment — this step is mandatory**: post it with `multica issue comment add %s` using the platform-correct non-inline mode from ## Comment Formatting (never inline `--content`). Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.", ctx.IssueID)
	}
	b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.WorkflowAssignKey, map[string]string{
		"assignment_intro":   "You are responsible for managing the issue status throughout your work, unless your Agent Identity forbids issue status changes.\n",
		"step_issue_get":     fmt.Sprintf("Run `multica issue get %s --output json` to understand your task", ctx.IssueID),
		"step_metadata_list": fmt.Sprintf("Run `multica issue metadata list %s --output json` to see what prior agents pinned — best-effort, empty `{}` and CLI failures are normal. See the `## Issue Metadata` section above for what to look for.", ctx.IssueID),
		"step_comment_read":  fmt.Sprintf("Run `multica issue comment list %s --recent 10 --output json` to catch up on recent active comment threads — this is mandatory, not optional. Earlier comments often carry context the issue body lacks (e.g. which repo to work in, the prior agent's findings, the reason the issue was reassigned to you). Skipping this step is the most common cause of agents acting on stale or incomplete instructions. Resolved threads come back folded — `--full` to expand. If the recent window shows that older context is needed, page older threads with the stderr `Next thread cursor:` values and the matching `--before` / `--before-id` flags until you have enough history.", ctx.IssueID),
		"step_in_progress":   fmt.Sprintf("Run `multica issue status %s in_progress` unless your Agent Identity forbids issue status changes; if it does, skip this step.", ctx.IssueID),
		"step_complete_task": "Complete the task within your Agent Identity boundaries. Do not investigate, implement, create issues, update issues, or delegate if your Agent Identity forbids that action; if your role is delegation-only, perform the allowed delegation work and stop once that outcome is delivered.",
		"step_final_comment": finalCommentStep,
		"step_metadata_exit": "Before exiting: only if this run produced a fact that clears the high bar (important AND likely to be re-read by future runs on this same issue, e.g. a new PR URL or deploy URL), or you noticed a metadata key from entry that is now stale, pin or clear it via `multica issue metadata set`/`delete`. Most runs write nothing here — that is the expected outcome, not a gap. When in doubt, do not write. See the `## Issue Metadata` section above for the full bar.",
		"step_in_review":     fmt.Sprintf("When done, run `multica issue status %s in_review` unless your Agent Identity forbids issue status changes; if it does, skip this step.", ctx.IssueID),
		"step_blocked":       fmt.Sprintf("If blocked, run `multica issue status %s blocked` unless your Agent Identity forbids issue status changes. Post a comment explaining the blocker unless your Agent Identity forbids issue comments.", ctx.IssueID),
	}))
	b.WriteString("\n")
}

// writeSubIssueCreation emits the Sub-issue Creation section (compressed
// to two short paragraphs).
func writeSubIssueCreation(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Sub-issue Creation\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.SubIssueCreateKey, nil))
	b.WriteString("\n")
}

// writeSkills emits the Skills section listing skill names + descriptions.
func writeSkills(b *strings.Builder, provider string, ctx TaskContextForEnv) {
	if len(ctx.AgentSkills) == 0 {
		return
	}
	b.WriteString("## Skills\n\n")
	var skillsList strings.Builder
	for _, skill := range ctx.AgentSkills {
		if desc := strings.TrimSpace(skill.Description); desc != "" {
			fmt.Fprintf(&skillsList, "- **%s** — %s\n", skill.Name, desc)
		} else {
			fmt.Fprintf(&skillsList, "- **%s**\n", skill.Name)
		}
	}
	switch provider {
	case "claude", "codebuddy":
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.SkillsNativeKey, map[string]string{
			"skills_list": skillsList.String(),
		}))
	case "codex", "copilot", "opencode", "openclaw", "pi", "cursor", "kimi", "kiro", "qoder", "antigravity":
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.SkillsNativeKey, map[string]string{
			"skills_list": skillsList.String(),
		}))
	case "hermes":
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.SkillsFallbackKey, map[string]string{
			"skills_list": skillsList.String(),
		}))
	default:
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.SkillsFallbackKey, map[string]string{
			"skills_list": skillsList.String(),
		}))
	}
	b.WriteString("\n")
}

// writeMentions emits the @mention side-effects section (compressed).
func writeMentions(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Mentions\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.MentionsKey, nil))
	b.WriteString("\n")
}

// writeAttachments emits the Attachments pointer.
func writeAttachments(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Attachments\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.AttachmentsKey, nil))
	b.WriteString("\n")
}

// writeAlwaysUseCLI emits the "must go through the multica CLI" guardrail
// (compressed).
func writeAlwaysUseCLI(b *strings.Builder, templates map[string]string) {
	b.WriteString("## Important: Always Use the `multica` CLI\n\n")
	b.WriteString(runtimeTemplate(templates, prompttmpl.AlwaysUseCLIKey, nil))
	b.WriteString("\n")
}

// writeOutput emits the kind-specific Output section.
func writeOutput(b *strings.Builder, kind taskKind, ctx TaskContextForEnv) {
	b.WriteString("## Output\n\n")
	switch kind {
	case kindAutopilotRunOnly:
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.OutputAutopilotKey, nil))
	case kindQuickCreate:
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.OutputQuickCreateKey, nil))
	case kindChat:
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.OutputChatKey, nil))
	default:
		noActionBlock := "⚠️ **Final results MUST be delivered via `multica issue comment add`.** The user does NOT see your terminal output, assistant chat text, or run logs — only comments on the issue. A task that finishes without a result comment is invisible to the user, even if the work itself was correct.\n\n"
		if ctx.IsSquadLeader {
			noActionBlock = "⚠️ **Final results MUST be delivered via `multica issue comment add`** — unless your outcome is `no_action`. When you evaluate a trigger and decide no action is needed, calling `multica squad activity <issue-id> no_action --reason \"...\"` alone is sufficient; you MUST exit without posting any comment. DO NOT post a comment that announces no_action, acknowledges another agent, or says you are exiting silently — such comments are noise. For all other outcomes (`action`, `failed`), a comment is still mandatory.\n\n"
		}
		b.WriteString(runtimeTemplate(ctx.PromptTemplates, prompttmpl.OutputIssueKey, map[string]string{
			"no_action_block": noActionBlock,
		}))
	}
}

// buildMetaSkillContentSlim is the post-MUL-3560 slim brief assembler.
// Gated by the `runtime_brief_slim` feature flag; only called from
// buildMetaSkillContent (runtime_config.go) when the flag is on.
//
// The Section × Kind matrix encoded below (skip = elide section, keep
// = always emit, △ = data-driven inside the helper):
//
//	Section               | comment | assign | autopilot | quick_create | chat
//	----------------------+---------+--------+-----------+--------------+------
//	Available Commands    |   full  |  full  |   full    |   minimal    | full
//	Comment Formatting    |    ✓    |   ✓    |     —     |      —       |  —
//	Repositories          |    △    |   △    |     △     |      —       |  △
//	Project Context       |    △    |   △    |     —     |      —       |  —
//	Issue Metadata        |    ✓    |   ✓    |     —     |      —       |  —
//	Instruction Precedence|    —    |   ✓    |     —     |      —       |  —
//	Sub-issue Creation    |    ✓    |   ✓    |     —     |      —       |  —
//	Skills                |    ✓    |   ✓    |     ✓    |      —       |  ✓
//	Mentions              |    ✓    |   ✓    |     —     |      —       |  —
//	Attachments           |    ✓    |   ✓    |     —     |      —       |  —
//
// Always-on rows — Header, Background Task Safety, Agent Identity,
// Workspace Init Prompt, Requesting User, Task Initiator, Workspace Context, Connected Apps,
// Workflow, Always Use CLI, Output — are shared by every kind and emitted
// unconditionally (or gated by their own data preconditions).
func buildMetaSkillContentSlim(provider string, ctx TaskContextForEnv) string {
	var b strings.Builder
	kind := classifyTask(ctx)

	writeHeader(&b, ctx.PromptTemplates)
	writeBackgroundTaskSafetySlim(&b, ctx.PromptTemplates)
	writeAgentIdentity(&b, ctx)
	writeWorkspaceInitPrompt(&b, ctx)
	writeRequestingUser(&b, ctx)
	writeTaskInitiator(&b, ctx)
	writeWorkspaceContext(&b, ctx)
	writeConnectedApps(&b, ctx)

	switch kind {
	case kindQuickCreate:
		writeAvailableCommandsQuickCreate(&b, ctx.PromptTemplates)
	default:
		writeAvailableCommands(&b, ctx.PromptTemplates)
	}

	if kind == kindCommentTriggered || kind == kindAssignmentTriggered {
		writeCommentFormatting(&b, ctx.PromptTemplates)
	}

	if kind != kindQuickCreate {
		writeRepositories(&b, ctx)
	}

	if kind.hasIssueContext() {
		writeProjectContext(&b, ctx)
		writeIssueMetadata(&b, ctx.PromptTemplates)
	}

	if kind == kindAssignmentTriggered {
		writeInstructionPrecedence(&b, ctx.PromptTemplates)
	}

	writeWorkflowHeader(&b)
	switch kind {
	case kindChat:
		writeWorkflowChat(&b, ctx.PromptTemplates)
	case kindQuickCreate:
		writeWorkflowQuickCreate(&b, ctx.PromptTemplates)
	case kindAutopilotRunOnly:
		writeWorkflowAutopilot(&b, ctx)
	case kindCommentTriggered:
		writeWorkflowComment(&b, provider, ctx)
	case kindAssignmentTriggered:
		writeWorkflowAssignment(&b, ctx)
	}

	if kind.hasIssueContext() && ctx.IssueID != "" {
		writeSubIssueCreation(&b, ctx.PromptTemplates)
	}

	if kind != kindQuickCreate {
		writeSkills(&b, provider, ctx)
	}

	if kind == kindCommentTriggered || kind == kindAssignmentTriggered {
		writeMentions(&b, ctx.PromptTemplates)
		writeAttachments(&b, ctx.PromptTemplates)
	}

	writeAlwaysUseCLI(&b, ctx.PromptTemplates)
	writeOutput(&b, kind, ctx)

	return b.String()
}
