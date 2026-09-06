package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/prompttmpl"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is threaded
// through to comment-triggered tasks' per-turn reply template; that template
// is provider-agnostic AND host-agnostic now (every OS → write a UTF-8 file,
// post with `--content-file`) because the shell-layer corruption it guards
// against is not specific to any one provider or host (MUL-2904, #4182).
func BuildPrompt(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	handoffBlock := ""
	if task.HandoffNote != "" {
		handoffBlock = "You were handed this issue with a handoff note. Treat it as the assigner's scoping instruction for this run; follow it before doing anything broader, and do not reply to it as if it were a comment:\n\n" +
			fmt.Sprintf("> %s\n\n", task.HandoffNote)
	}
	return prompttmpl.Render(templateForTask(task.PromptTemplates, prompttmpl.AssignmentPromptKey), map[string]string{
		"issue_id":      task.IssueID,
		"handoff_block": handoffBlock,
	})
}

// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The
// user typed a single natural-language sentence in the create-issue modal;
// the agent's job is to translate it into one `multica issue create` CLI
// invocation, using its judgment to decide whether fetching referenced URLs
// would produce a better issue. No issue exists yet, so the agent must NOT
// call `multica issue get` or attempt to comment — there's nothing to read
// or reply to.
func buildQuickCreatePrompt(task Task) string {
	assigneeBlock := ""
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	switch {
	case task.SquadID != "":
		// The user opened quick-create with a SQUAD selected. The task
		// runs on the squad's leader agent, but the squad is the expected
		// owner — assigning to the leader would mask the squad's
		// delegation flow. Always point the default at the squad UUID.
		if task.SquadName != "" {
			assigneeBlock = fmt.Sprintf("    - When the user did NOT name an assignee, default to the picker SQUAD %q: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadName, task.SquadID)
		} else {
			assigneeBlock = fmt.Sprintf("    - When the user did NOT name an assignee, default to the picker SQUAD: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadID)
		}
	case agentID != "":
		assigneeBlock = fmt.Sprintf("    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	case agentName != "":
		assigneeBlock = fmt.Sprintf("    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	default:
		assigneeBlock = "    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n"
	}

	projectBlock := "omit. The platform will route the issue to the workspace default.\n"
	if task.ProjectID != "" {
		if task.ProjectTitle != "" {
			projectBlock = fmt.Sprintf("required for this run. Pass `--project %q` so the new issue lands in project %q (the user picked it in the quick-create modal). Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID, task.ProjectTitle)
		} else {
			projectBlock = fmt.Sprintf("required for this run. Pass `--project %q` so the new issue lands in the project the user picked in the quick-create modal. Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID)
		}
	}
	parentBlock := "omit unless the quick-create modal pinned a parent issue.\n"
	if task.ParentIssueID != "" {
		if task.ParentIssueIdentifier != "" {
			parentBlock = fmt.Sprintf("required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of %s (the user opened quick-create from that issue's \"Add sub issue\" entry). Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID, task.ParentIssueIdentifier)
		} else {
			parentBlock = fmt.Sprintf("required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of the parent the user picked in the quick-create modal. Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID)
		}
	}
	return prompttmpl.Render(templateForTask(task.PromptTemplates, prompttmpl.QuickCreateKey), map[string]string{
		"user_input":     task.QuickCreatePrompt,
		"assignee_block": assigneeBlock,
		"project_block":  projectBlock,
		"parent_block":   parentBlock,
	})
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
	triggerCommentBlock := ""
	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		fmt.Fprintf(&sb, "> %s\n\n", task.TriggerCommentContent)
		if task.TriggerAuthorType == "agent" {
			sb.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
		if task.Agent != nil && strings.Contains(task.Agent.Instructions, "## Squad Operating Protocol") {
			fmt.Fprintf(&sb, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
		triggerCommentBlock = sb.String()
	}
	// Comment-reading pointer. Warm path with new comments: issue-wide
	// since-delta count, but steer the agent to read the triggering thread
	// first. Warm resumed path with no new comments: the trigger is already
	// injected, so don't force a duplicate thread read. Cold path: read the
	// triggering thread, not the flat timeline. Final fallback (no trigger id,
	// shouldn't happen here): plain read.
	commentReadHint := ""
	if hint := execenv.BuildNewCommentsHint(task.PromptTemplates, task.IssueID, task.TriggerCommentID, task.TriggerThreadID, task.NewCommentsSince, task.NewCommentCount); hint != "" {
		commentReadHint = hint
	} else if task.PriorSessionID != "" {
		commentReadHint = execenv.BuildResumedCommentsHint(task.PromptTemplates, task.IssueID, task.TriggerCommentID, task.TriggerThreadID)
	} else if cold := execenv.BuildColdCommentsHint(task.PromptTemplates, task.IssueID, task.TriggerCommentID, task.TriggerThreadID); cold != "" {
		commentReadHint = cold
	} else {
		commentReadHint = fmt.Sprintf("Read the discussion: `multica issue comment list %s --recent 10 --output json` (resolved threads come back folded — `--full` to expand).\n\n", task.IssueID)
	}
	return prompttmpl.Render(templateForTask(task.PromptTemplates, prompttmpl.CommentPromptKey), map[string]string{
		"issue_id":                   task.IssueID,
		"trigger_comment_block":      triggerCommentBlock,
		"comment_read_hint":          commentReadHint,
		"comment_reply_instructions": execenv.BuildCommentReplyInstructions(provider, task.PromptTemplates, task.IssueID, task.TriggerCommentID),
	})
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	channelContextBlock := ""
	// Channel awareness (MUL-3871). When the session is backed by an IM channel,
	// the agent must KNOW it is operating inside that channel — otherwise an ask
	// like "what did you just talk about" sends it to read Multica instead of the
	// Slack conversation. State it explicitly, point reads at the channel (not
	// Multica), and teach the two read commands, telling the agent which to start
	// with based on where it was @mentioned. A web-only chat session gets no such
	// block — its history is the Multica chat_session the agent already resumes.
	if task.ChatChannelType != "" {
		platform := channelDisplayName(task.ChatChannelType)
		var sb strings.Builder
		fmt.Fprintf(&sb, "You are operating inside a %s conversation — not the Multica web app. This conversation and its history live in %s, NOT in Multica; never look in Multica issues or comments for it. The message below may be only what triggered you. Read the conversation with:\n", platform, platform)
		sb.WriteString("- `multica chat history --output json` — the channel overview: recent top-level messages, each thread tagged with a `thread_id` and `reply_count`. It does NOT expand thread contents.\n")
		sb.WriteString("- `multica chat thread [<thread_id>] --output json` — read one thread's messages; omit the id to read the thread you are in, or pass a `thread_id` from the overview to read a specific thread.\n")
		if task.ChatInThread {
			sb.WriteString("You were @mentioned inside a thread: start with `multica chat thread` to read it; if you need the wider channel, run `multica chat history` and open a specific thread with `multica chat thread <thread_id>`.\n")
		} else {
			sb.WriteString("You were @mentioned at the channel top level: start with `multica chat history` to see the channel, then read a specific thread's contents with `multica chat thread <thread_id>`.\n")
		}
		// These reads are the agent's private context-gathering; narrating them
		// into a chat reply reads as noise (the user reported every reply being
		// prefixed with "我先读取…"). Tell the agent to keep them out of its answer.
		sb.WriteString("Do these reads SILENTLY as an internal step — they are how you gather context, not part of your answer. Do NOT narrate them: your reply must not begin with what you are about to read or just read (no \"我先读取…\" / \"let me read the history / open the thread\"). Reply to the user with your answer only.\n\n")
		channelContextBlock = sb.String()
	}
	selectedSkillsBlock := ""
	if task.Agent != nil && len(task.Agent.Skills) > 0 {
		refs := ExtractSlashSkills(task.ChatMessage)
		if len(refs) > 0 {
			agentSkills := make(map[string]string, len(task.Agent.Skills))
			for _, s := range task.Agent.Skills {
				agentSkills[s.ID] = s.Name
			}

			selected := make([]string, 0, len(refs))
			seen := make(map[string]struct{}, len(refs))
			for _, ref := range refs {
				name, ok := agentSkills[ref.ID]
				if !ok {
					continue
				}
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				selected = append(selected, name)
			}

			if len(selected) > 0 {
				var sb strings.Builder
				sb.WriteString("Explicitly selected skills:\n")
				for _, name := range selected {
					fmt.Fprintf(&sb, "- %s\n", name)
				}
				sb.WriteString("\n")
				selectedSkillsBlock = sb.String()
			}
		}
	}
	attachmentsBlock := ""
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		var sb strings.Builder
		sb.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&sb, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&sb, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		sb.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
		sb.WriteString("When creating an issue that should preserve one of these attachments, pass `--attachment-id <id>` to `multica issue create` in addition to keeping the attachment markdown inline.\n")
		attachmentsBlock = sb.String()
	}
	return prompttmpl.Render(templateForTask(task.PromptTemplates, prompttmpl.ChatPromptKey), map[string]string{
		"channel_context_block": channelContextBlock,
		"selected_skills_block": selectedSkillsBlock,
		"user_message":          task.ChatMessage,
		"attachments_block":     attachmentsBlock,
	})
}

// channelDisplayName renders a chat_channel_type for prompt copy.
func channelDisplayName(channelType string) string {
	switch channelType {
	case "slack":
		return "Slack"
	default:
		return channelType
	}
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	autopilotIDLine := ""
	if task.AutopilotID != "" {
		autopilotIDLine = fmt.Sprintf("Autopilot ID: %s\n", task.AutopilotID)
	}
	autopilotTitleLine := ""
	if task.AutopilotTitle != "" {
		autopilotTitleLine = fmt.Sprintf("Autopilot title: %s\n", task.AutopilotTitle)
	}
	autopilotSourceLine := ""
	if task.AutopilotSource != "" {
		autopilotSourceLine = fmt.Sprintf("Trigger source: %s\n", task.AutopilotSource)
	}
	triggerPayloadBlock := ""
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		triggerPayloadBlock = fmt.Sprintf("Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	autopilotInstructions := "No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n"
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		autopilotInstructions = task.AutopilotDescription + "\n\n"
	} else if task.AutopilotTitle != "" {
		autopilotInstructions = task.AutopilotTitle + "\n\n"
	}
	startInstruction := "Complete the instructions above.\n"
	if task.AutopilotID != "" {
		startInstruction = fmt.Sprintf("Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	}
	return prompttmpl.Render(templateForTask(task.PromptTemplates, prompttmpl.AutopilotPromptKey), map[string]string{
		"autopilot_run_id":       task.AutopilotRunID,
		"autopilot_id_line":      autopilotIDLine,
		"autopilot_title_line":   autopilotTitleLine,
		"autopilot_source_line":  autopilotSourceLine,
		"trigger_payload_block":  triggerPayloadBlock,
		"autopilot_instructions": autopilotInstructions,
		"start_instruction":      startInstruction,
	})
}

func templateForTask(templates map[string]string, key string) string {
	if templates != nil {
		if value, ok := templates[key]; ok {
			return value
		}
	}
	return prompttmpl.DefaultTemplate(key)
}
