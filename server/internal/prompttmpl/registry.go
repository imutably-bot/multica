package prompttmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	WorkspaceInitKey     = "workspace_init"
	RuntimeHeaderKey     = "runtime_header"
	BackgroundSafetyKey  = "runtime_background_task_safety"
	AssignmentPromptKey  = "assignment_prompt"
	CommentPromptKey     = "comment_prompt"
	CommentNewHintKey    = "comment_new_comments_hint"
	CommentResumedKey    = "comment_resumed_hint"
	CommentColdKey       = "comment_cold_hint"
	CommentReplyKey      = "comment_reply_instructions"
	ChatPromptKey        = "chat_prompt"
	QuickCreateKey       = "quick_create_prompt"
	AutopilotPromptKey   = "autopilot_prompt"
	IssueMetadataKey     = "runtime_issue_metadata"
	InstructionPrecKey   = "runtime_instruction_precedence"
	AvailableCmdsKey     = "runtime_available_commands"
	AvailableCmdsQCKey   = "runtime_available_commands_quick_create"
	CommentFormatKey     = "runtime_comment_formatting"
	RepositoriesKey      = "runtime_repositories"
	ProjectContextKey    = "runtime_project_context"
	SubIssueCreateKey    = "runtime_sub_issue_creation"
	SkillsNativeKey      = "runtime_skills_native"
	SkillsFallbackKey    = "runtime_skills_fallback"
	MentionsKey          = "runtime_mentions"
	AttachmentsKey       = "runtime_attachments"
	AlwaysUseCLIKey      = "runtime_always_use_cli"
	OutputAutopilotKey   = "runtime_output_autopilot"
	OutputQuickCreateKey = "runtime_output_quick_create"
	OutputChatKey        = "runtime_output_chat"
	OutputIssueKey       = "runtime_output_issue"
)

type Scope string

const (
	ScopeWorkspace Scope = "workspace"
	ScopeAgent     Scope = "agent"
)

type Definition struct {
	Key                string   `json:"key"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	SupportedScopes    []Scope  `json:"supported_scopes"`
	SupportedVariables []string `json:"supported_variables"`
	DefaultTemplate    string   `json:"default_template"`
}

var placeholderPattern = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

var definitions = []Definition{
	{
		Key:             WorkspaceInitKey,
		Title:           "Workspace init prompt",
		Description:     "Short prompt injected near the top of generated AGENTS.md / CLAUDE.md workdir guidance.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"agent_name",
			"workspace_name",
			"issue_id",
			"chat_id",
			"user_name",
			"repo_url",
			"task_type",
		},
		DefaultTemplate: "You are {{agent_name}}, an AI agent that helps users in {{workspace_name}}. Be concise, helpful, and action-oriented. Use available tools when needed and ask clarifying questions if something is unclear.",
	},
	{
		Key:                RuntimeHeaderKey,
		Title:              "Runtime header",
		Description:        "Top header and one-line intro for generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "# Multica Agent Runtime\n\nYou are a coding agent in the Multica platform. Use the `multica` CLI to interact with the platform.\n",
	},
	{
		Key:                BackgroundSafetyKey,
		Title:              "Runtime background task safety section",
		Description:        "Safety block for handling background work in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Multica marks the task terminal when your top-level turn exits — any background work still running may be orphaned and its result lost.\n\n- Do NOT end your turn while background tasks, async subagents, background shell commands, or detached tool calls are still running.\n- If a tool response says to wait for a future notification/reminder, do not rely on that in Multica-managed runs — block on the appropriate wait / output / collect operation before exiting.\n- If you can't observe a background task's result, run the work synchronously instead.\n",
	},
	{
		Key:             AssignmentPromptKey,
		Title:           "Assignment task prompt",
		Description:     "Top-level prompt used for assignment-triggered issue tasks.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"issue_id",
			"handoff_block",
		},
		DefaultTemplate: "You are running as a local coding agent for a Multica workspace.\n\nYour assigned issue ID is: {{issue_id}}\n\n{{handoff_block}}Start by running `multica issue get {{issue_id}} --output json` to understand your task, then complete it.\nFor comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). Start with `multica issue comment list {{issue_id}} --recent 10 --output json` to read the 10 most recently active threads, then page older threads via the stderr `Next thread cursor: ...` line and the matching `--before` / `--before-id` until you have enough history. Resolved threads come back folded — `--full` to expand. `--since <RFC3339>` is still available for incremental polling and may combine with `--recent`.\n",
	},
	{
		Key:             CommentPromptKey,
		Title:           "Comment task prompt",
		Description:     "Top-level prompt used for comment-triggered issue tasks.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"issue_id",
			"trigger_comment_block",
			"comment_read_hint",
			"comment_reply_instructions",
		},
		DefaultTemplate: "You are running as a local coding agent for a Multica workspace.\n\nYour assigned issue ID is: {{issue_id}}\n\n{{trigger_comment_block}}Start by running `multica issue get {{issue_id}} --output json` to understand your task, then decide how to proceed.\n\n{{comment_read_hint}}{{comment_reply_instructions}}",
	},
	{
		Key:             CommentNewHintKey,
		Title:           "Comment warm-start hint",
		Description:     "Hint shown when a comment-triggered task has newer issue comments since the prior run.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"new_comment_count",
			"issue_id",
			"thread_id",
			"new_comments_since",
		},
		DefaultTemplate: "{{new_comment_count}} new comment(s) on this issue since your last run — don't read them all blindly. Start with the thread your triggering comment is in: `multica issue comment list {{issue_id}} --thread {{thread_id}} --since {{new_comments_since}} --output json` (swap `--since` for `--tail 30` if you need the full thread, not just the delta). Only if you need context from the other threads, catch up issue-wide: `multica issue comment list {{issue_id}} --since {{new_comments_since}} --output json`.\n\n",
	},
	{
		Key:             CommentResumedKey,
		Title:           "Comment resumed-session hint",
		Description:     "Hint shown when resuming a comment-triggered session with no new issue comments.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"thread_id",
			"trigger_comment_id",
			"issue_id",
		},
		DefaultTemplate: "You're resuming the prior session, and the triggering comment is already included above. No other new comments on this issue since your last run. Use the active thread anchor `{{thread_id}}` and triggering comment ID `{{trigger_comment_id}}`. If your reply depends on thread context, do not rely only on resumed session memory — first pull the triggering conversation with: `multica issue comment list {{issue_id}} --thread {{thread_id}} --tail 30 --output json`.\n\n",
	},
	{
		Key:             CommentColdKey,
		Title:           "Comment cold-start hint",
		Description:     "Hint shown when a comment-triggered task starts cold with no prior session anchor.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"issue_id",
			"thread_id",
		},
		DefaultTemplate: "Read the triggering conversation first: `multica issue comment list {{issue_id}} --thread {{thread_id}} --tail 30 --output json` (that thread's root + its 30 newest replies). Need cross-thread background? `multica issue comment list {{issue_id}} --recent 10 --output json` (resolved threads come back folded — `--full` to expand).\n\n",
	},
	{
		Key:             CommentReplyKey,
		Title:           "Comment reply instructions",
		Description:     "Instructions for safely posting a comment reply through the Multica CLI.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"issue_id",
			"trigger_comment_id",
		},
		DefaultTemplate: "If you decide to reply, post it as a comment — always use the trigger comment ID below, do NOT reuse --parent values from previous turns in this session.\n\nWrite the reply body to a UTF-8 file with your file-write tool first, then post it with `--content-file`. Do NOT use inline `--content`; the shell rewrites unescaped backticks, `$()`, `$VAR`, or quotes in the body before the CLI receives them. Do NOT use `--content-stdin` with a HEREDOC either — when extra flags (e.g. `--assignee`, `--project` on `multica issue create`) accompany the command, the bash heredoc/flag boundary is fragile and flags can be silently swallowed into the stdin stream while the command still exits 0 (see GitHub #4182, OXY-78 / OXY-76). It is also easy to lose formatting or compress a structured reply into one line with inline forms.\n\nUse this form, preserving the same issue ID and --parent value:\n\n    # 1. Write the reply body to a UTF-8 file (e.g. reply.md) with your file-write tool.\n    # 2. Post the comment:\n    multica issue comment add {{issue_id}} --parent {{trigger_comment_id}} --content-file ./reply.md\n    # 3. Remove the temp file so a later run does not pick up stale content:\n    rm ./reply.md\n\nDo NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
	},
	{
		Key:             ChatPromptKey,
		Title:           "Chat task prompt",
		Description:     "Top-level prompt used for direct chat tasks.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"channel_context_block",
			"selected_skills_block",
			"user_message",
			"attachments_block",
		},
		DefaultTemplate: "You are running as a chat assistant for a Multica workspace.\nA user is chatting with you directly. Respond to their message.\n\n{{channel_context_block}}{{selected_skills_block}}User message:\n{{user_message}}\n{{attachments_block}}",
	},
	{
		Key:             QuickCreateKey,
		Title:           "Quick-create prompt",
		Description:     "Prompt used for issue quick-create requests before any issue exists.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"user_input",
			"assignee_block",
			"project_block",
			"parent_block",
		},
		DefaultTemplate: "You are running as a quick-create assistant for a Multica workspace.\n\nA user captured the following input via the quick-create modal. There is NO existing issue. Your job is to create a well-formed issue from this input with a single `multica issue create` command.\n\nUser input:\n> {{user_input}}\n\nField rules:\n\n- **title**: required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n- **description**: The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n- **assignee**:\n    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace member list --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. Squads are first-class assignees too — a squad name (e.g. \"Super Human\") routes work to the squad leader, who then delegates. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent or squad) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names. Do not keep that routing wrapper or `@Name` in the description unless it is a true CC-style notification rather than ownership. If the matched entity is a squad, pass the squad's `id` as `--assignee-id`, not the leader agent's id.\n{{assignee_block}}- **project**: {{project_block}}- **parent**: {{parent_block}}- **status**: omit (defaults to `todo`).\n- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\nOutput format:\n- Run exactly one `multica issue create --output json` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n- Parse the JSON response to read the created issue's `identifier` (preferred) or `id` (fallback). Do not scrape human output and do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n- After success, print exactly one line: `Created <identifier-or-id>: <title>` and exit. No commentary, no follow-up tool calls.\n- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n- On CLI error or JSON parse error, exit with the error as the only output. The platform writes a failure notification automatically.\n",
	},
	{
		Key:             AutopilotPromptKey,
		Title:           "Autopilot prompt",
		Description:     "Top-level prompt used for run-only autopilot tasks.",
		SupportedScopes: []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{
			"autopilot_run_id",
			"autopilot_id_line",
			"autopilot_title_line",
			"autopilot_source_line",
			"trigger_payload_block",
			"autopilot_instructions",
			"start_instruction",
		},
		DefaultTemplate: "You are running as a local coding agent for a Multica workspace.\n\nThis task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\nAutopilot run ID: {{autopilot_run_id}}\n{{autopilot_id_line}}{{autopilot_title_line}}{{autopilot_source_line}}{{trigger_payload_block}}\nAutopilot instructions:\n{{autopilot_instructions}}{{start_instruction}}Do not run `multica issue get`; this run does not have an issue ID.\n",
	},
	{
		Key:                IssueMetadataKey,
		Title:              "Runtime issue metadata section",
		Description:        "Guidance block for how agents should read and write issue metadata in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "`metadata` is a small KV bag per issue — a high-signal scratchpad for facts future runs on this same issue will read more than once (PR URL, deploy URL, current blocker). Most runs pin **zero** new keys; that is the expected case.\n\n- **Read on entry.** Metadata is hints, not truth: latest comment / code wins on conflict. Empty `{}` is normal.\n- **Write on exit.** Pin only if BOTH: (a) materially important to this issue, AND (b) a future run is likely to re-read it. Otherwise leave the bag alone. Stale keys: overwrite with the new value or `multica issue metadata delete`.\n- **What NOT to pin.** No secrets, tokens, or API keys. No logs or comment summaries. No runtime bookkeeping (attempts, run timestamps, agent ids). No single-run details — those belong in the result comment.\n- **Recommended keys** (use snake_case ASCII; reuse these names so queries stay consistent): `pr_url`, `pr_number`, `pipeline_status`, `deploy_url`, `external_issue_url`, `waiting_on`, `blocked_reason`, `decision`.\n",
	},
	{
		Key:                AvailableCmdsKey,
		Title:              "Runtime available commands section",
		Description:        "Core Multica CLI command reference shown in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Prefer `--output json` for structured data. The default brief lists only the core agent loop and common issue create/update tasks; for everything else run `multica --help` or `multica <command> --help`.\n\n### Core\n- `multica issue get <id> --output json` — full issue.\n- `multica issue comment list <issue-id> [--thread <comment-id> [--tail N] | --recent N] [--before <ts> --before-id <uuid>] [--since <RFC3339>] [--full] --output json` — thread-aware comment reads. Resolved threads come back folded by default on complete-thread reads (default list, `--recent`, `--thread` without `--tail`); pass `--full` to expand. Page older replies / threads with `--before`/`--before-id` (stderr labels: `Next reply cursor`, `Next thread cursor`); `--help` for full semantics.\n- `multica issue create --title \"...\" [--description-file <path>] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <RFC3339>] [--attachment <path>]` — create an issue. For agent-authored long descriptions prefer `--description-file <path>` (heredoc stdin can swallow trailing flags, #4182).\n- `multica issue update <id> [--title X] [--description-file <path>] [--priority X] [--status X] [--assignee X] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <RFC3339>]` — update fields; pass `--parent \"\"` to clear parent.\n- `multica issue status <id> <status>` — flip status (todo / in_progress / in_review / done / blocked / backlog / cancelled).\n- `multica issue children <id> [--output json]` — list a parent's sub-issues grouped by stage.\n- `multica issue comment add <issue-id> [--content \"...\" | --content-file <path> | --content-stdin] [--parent <comment-id>] [--attachment <path>]` — post a comment. Agent-authored bodies MUST use `--content-file`. `multica issue comment add --help` for full flags.\n- `multica issue metadata list <issue-id> [--output json]` — list KV metadata.\n- `multica issue metadata set <issue-id> --key <k> --value <v> [--type string|number|bool]` — pin or overwrite a key.\n- `multica issue metadata delete <issue-id> --key <k>` — remove a key.\n- `multica repo checkout <url> [--ref <branch-or-sha>]` — git worktree on a dedicated branch.\n\n### Squad maintenance\n- `multica squad member set-role <squad-id> --member-id <id> --member-type <agent|member> --role <role> [--output json]` — change role in place (use this instead of remove+add).\n",
	},
	{
		Key:                AvailableCmdsQCKey,
		Title:              "Runtime available commands section (quick-create)",
		Description:        "Minimal CLI reference shown for quick-create runtime briefs.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "**Use `--output json` for structured data.** For anything beyond `issue create`, run `multica --help` or `multica <command> --help`.\n\n### Core\n- `multica issue create --title \"...\" [--description \"...\" | --description-file <path> | --description-stdin] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <RFC3339>] [--attachment <path>]` — Create a new issue; `--attachment` may be repeated. For agent-authored long descriptions, prefer `--description-file <path>` over `--description-stdin` (flags after a HEREDOC terminator can be silently swallowed, #4182).\n",
	},
	{
		Key:                CommentFormatKey,
		Title:              "Runtime comment formatting section",
		Description:        "Comment-posting guardrails shown in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "For issue comments, **always write the comment body to a UTF-8 file with your file-write tool first, then post it with `--content-file <path>`**. Never use inline `--content` for agent-authored comments — the shell rewrites backticks / `$()` / quotes in the body (MUL-2904). Never use `--content-stdin` with a HEREDOC alongside other flags either — the heredoc/flag boundary is fragile and flags get silently swallowed (#4182). Keep the same `--parent` value from the trigger comment when replying. Delete the temp file (`rm ./reply.md`) after posting; do not rely on `\\n` escapes.\n",
	},
	{
		Key:                RepositoriesKey,
		Title:              "Runtime repositories section",
		Description:        "Repository list block for generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{"repo_list"},
		DefaultTemplate:    "Available in this workspace — `multica repo checkout <url> [--ref <branch-or-sha>]` to fetch (creates a git worktree on a dedicated branch).\n\n{{repo_list}}",
	},
	{
		Key:                ProjectContextKey,
		Title:              "Runtime project context section",
		Description:        "Project context block for generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{"project_title_block", "project_description_block", "project_resources_block"},
		DefaultTemplate:    "{{project_title_block}}{{project_description_block}}{{project_resources_block}}",
	},
	{
		Key:                InstructionPrecKey,
		Title:              "Runtime instruction precedence section",
		Description:        "Guardrail stating that agent identity instructions override the generated assignment workflow.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Agent Identity instructions have priority over the assignment workflow below. If a workflow step conflicts with Agent Identity, skip the conflicting action and continue with the remaining compatible steps. Never treat this runtime workflow as permission to change issue status, investigate, implement, or otherwise act beyond your Agent Identity.\n",
	},
	{
		Key:                SubIssueCreateKey,
		Title:              "Runtime sub-issue creation section",
		Description:        "Guidance block for creating and staging sub-issues in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "**Choosing `--status` when creating sub-issues.** `--status todo` = **start now** (default — agent assignees fire immediately). `--status backlog` = **wait**, then promote later with `multica issue status <child-id> todo`. Parallel children: all `--status todo`. Strict serial 1→2→3: only Step 1 `todo`, Steps 2/3 `--status backlog` from the start.\n\n**Ordering with stages.** For phased plans, group children with `--stage <N>` (N ≥ 1) instead of hand-promoting the backlog chain — stage members run together, and the parent wakes once per stage. Use `--stage k --status backlog` for later stages, then `multica issue children <id>` to inspect groupings before promoting. Reach for stages whenever a plan has more than one step or a step must wait for a group.\n",
	},
	{
		Key:                SkillsNativeKey,
		Title:              "Runtime skills section (native discovery)",
		Description:        "Skills section for vendors that discover skills natively from their own project path.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{"skills_list"},
		DefaultTemplate:    "You have the following skills installed (discovered automatically):\n\n{{skills_list}}",
	},
	{
		Key:                SkillsFallbackKey,
		Title:              "Runtime skills section (fallback discovery)",
		Description:        "Skills section for vendors that rely on the .agent_context fallback path.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{"skills_list"},
		DefaultTemplate:    "Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n{{skills_list}}",
	},
	{
		Key:                MentionsKey,
		Title:              "Runtime mentions section",
		Description:        "Guidance block for side-effecting mention links in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Mention links are **side-effecting actions**:\n\n- `[MUL-123](mention://issue/<issue-id>)` — clickable link (no side effect)\n- `[@Name](mention://member/<user-id>)` — **notifies a human**\n- `[@Name](mention://agent/<agent-id>)` — **enqueues a new run for that agent**\n\n### When NOT to use a mention link\n\nDefault: NO mention. Replying to another agent that just spoke to you, or thanking / acknowledging / signing off — **end with no mention at all**. An accidental `@mention` restarts an agent-to-agent loop and costs the user money.\n\n### When a mention IS appropriate\n\nEscalating to a human owner not yet involved; delegating a concrete new sub-task to another agent for the first time; or when the user explicitly asks to loop someone in. Otherwise **don't mention**. Silence ends conversations.\n",
	},
	{
		Key:                AttachmentsKey,
		Title:              "Runtime attachments section",
		Description:        "Guidance block for how agents should access attachments in generated CLAUDE.md / AGENTS.md.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Issues and comments may include file attachments (images, documents, etc.).\nWhen a task includes attachment IDs and you need the files, inspect `multica attachment --help` and use the authenticated CLI path. Do not open Multica resource URLs directly.\n",
	},
	{
		Key:                AlwaysUseCLIKey,
		Title:              "Runtime always-use-CLI section",
		Description:        "Guardrail block stating that Multica resources must be accessed through the CLI.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "Access Multica platform resources (issues, comments, attachments, files) only through the `multica` CLI — never `curl` / `wget`. For any operation the CLI doesn't cover, post a comment mentioning the workspace owner rather than working around it.\n",
	},
	{
		Key:                OutputAutopilotKey,
		Title:              "Runtime output section (autopilot)",
		Description:        "Output guidance for run-only autopilot runtime briefs.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "This is a run-only autopilot task, so there may be no issue comment to post. Your final assistant output is captured automatically as the autopilot run result. Keep it concise and state the outcome.\n",
	},
	{
		Key:                OutputQuickCreateKey,
		Title:              "Runtime output section (quick-create)",
		Description:        "Output guidance for quick-create runtime briefs.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "This is a quick-create task. There is NO existing issue to comment on. Your final stdout is captured automatically and the platform writes the user's success/failure inbox notification based on whether `multica issue create` succeeded.\n\n- Do NOT call `multica issue comment add` — the issue you just created has no conversation context for this run.\n- Print exactly one final line: `Created <identifier-or-id>: <title>` after a successful `multica issue create`. Use the created issue's `identifier` from JSON output when available; otherwise use its `id`. Do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n- On CLI failure, exit with the CLI error as the only output. The platform translates that into a `quick_create_failed` inbox item carrying the original prompt for the user.\n",
	},
	{
		Key:                OutputChatKey,
		Title:              "Runtime output section (chat)",
		Description:        "Output guidance for chat runtime briefs.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{},
		DefaultTemplate:    "This is a chat session. Your reply is delivered directly to the chat window the user is reading.\n",
	},
	{
		Key:                OutputIssueKey,
		Title:              "Runtime output section (issue tasks)",
		Description:        "Output guidance for issue-based runtime briefs.",
		SupportedScopes:    []Scope{ScopeWorkspace, ScopeAgent},
		SupportedVariables: []string{"no_action_block"},
		DefaultTemplate:    "{{no_action_block}}**Post exactly ONE comment per run — your final result, before this turn exits.** Do NOT post progress updates, plans, or \"here's what I'm about to do next\" as comments while you work; keep all planning and progress in your own reasoning.\n\nKeep comments concise and natural — state the outcome, not the process (good: \"Fixed the login redirect. PR: https://...\"; bad: numbered process logs).\n",
	},
}

var definitionsByKey = func() map[string]Definition {
	out := make(map[string]Definition, len(definitions))
	for _, def := range definitions {
		out[def.Key] = def
	}
	return out
}()

func Definitions() []Definition {
	out := make([]Definition, len(definitions))
	copy(out, definitions)
	return out
}

func DefinitionByKey(key string) (Definition, bool) {
	def, ok := definitionsByKey[key]
	return def, ok
}

func DefaultTemplate(key string) string {
	if def, ok := definitionsByKey[key]; ok {
		return def.DefaultTemplate
	}
	return ""
}

func SupportsScope(key string, scope Scope) bool {
	def, ok := definitionsByKey[key]
	if !ok {
		return false
	}
	for _, s := range def.SupportedScopes {
		if s == scope {
			return true
		}
	}
	return false
}

func ValidateTemplate(key, template string, scope Scope) error {
	def, ok := definitionsByKey[key]
	if !ok {
		return fmt.Errorf("unknown prompt template key %q", key)
	}
	if !SupportsScope(key, scope) {
		return fmt.Errorf("prompt template %q does not support %s scope", key, scope)
	}
	allowed := make(map[string]struct{}, len(def.SupportedVariables))
	for _, name := range def.SupportedVariables {
		allowed[name] = struct{}{}
	}
	for _, match := range placeholderPattern.FindAllStringSubmatch(template, -1) {
		if len(match) < 2 {
			continue
		}
		if _, ok := allowed[match[1]]; !ok {
			return fmt.Errorf("prompt template %q uses unsupported variable {{%s}}", key, match[1])
		}
	}
	return nil
}

func Render(template string, values map[string]string) string {
	if template == "" {
		return ""
	}
	return placeholderPattern.ReplaceAllStringFunc(template, func(raw string) string {
		match := placeholderPattern.FindStringSubmatch(raw)
		if len(match) < 2 {
			return raw
		}
		if v, ok := values[match[1]]; ok {
			return v
		}
		return ""
	})
}

func EffectiveTemplates(workspaceOverrides, agentOverrides map[string]string) map[string]string {
	out := make(map[string]string, len(definitions))
	for _, def := range definitions {
		out[def.Key] = def.DefaultTemplate
		if v, ok := workspaceOverrides[def.Key]; ok {
			out[def.Key] = v
		}
		if v, ok := agentOverrides[def.Key]; ok {
			out[def.Key] = v
		}
	}
	return out
}

func ExtractWorkspaceOverridesFromRaw(settings []byte, legacyInitPrompt string) map[string]string {
	overrides := ExtractOverridesFromRaw(settings)
	if _, ok := overrides[WorkspaceInitKey]; !ok && strings.TrimSpace(legacyInitPrompt) != "" {
		overrides[WorkspaceInitKey] = legacyInitPrompt
	}
	return overrides
}

func ExtractOverridesFromRaw(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return map[string]string{}
	}
	return ExtractOverridesFromObject(root)
}

func ExtractOverridesFromObject(root map[string]any) map[string]string {
	if len(root) == 0 {
		return map[string]string{}
	}
	raw, ok := root["prompt_templates"]
	if !ok {
		return map[string]string{}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(obj))
	for key, value := range obj {
		if s, ok := value.(string); ok {
			out[key] = s
		}
	}
	return out
}

func ValidateOverridesFromObject(root map[string]any, scope Scope) error {
	raw, ok := root["prompt_templates"]
	if !ok || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("prompt_templates must be an object")
	}
	keys := make([]string, 0, len(obj))
	for key, value := range obj {
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("prompt_templates.%s must be a string", key)
		}
		if err := ValidateTemplate(key, s, scope); err != nil {
			return err
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return nil
}
