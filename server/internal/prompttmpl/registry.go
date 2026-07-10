package prompttmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	WorkspaceInitKey    = "workspace_init"
	AssignmentPromptKey = "assignment_prompt"
	CommentPromptKey    = "comment_prompt"
	CommentNewHintKey   = "comment_new_comments_hint"
	CommentResumedKey   = "comment_resumed_hint"
	CommentColdKey      = "comment_cold_hint"
	CommentReplyKey     = "comment_reply_instructions"
	ChatPromptKey       = "chat_prompt"
	QuickCreateKey      = "quick_create_prompt"
	AutopilotPromptKey  = "autopilot_prompt"
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
