package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/prompttmpl"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type PromptTemplateDescriptorResponse struct {
	Key                string   `json:"key"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	SupportedScopes    []string `json:"supported_scopes"`
	SupportedVariables []string `json:"supported_variables"`
	DefaultTemplate    string   `json:"default_template"`
	WorkspaceOverride  *string  `json:"workspace_override,omitempty"`
	AgentOverride      *string  `json:"agent_override,omitempty"`
	BaseTemplate       string   `json:"base_template"`
	BaseSource         string   `json:"base_source"`
	EffectiveTemplate  string   `json:"effective_template"`
	EffectiveSource    string   `json:"effective_source"`
}

type PromptTemplateListResponse struct {
	Templates []PromptTemplateDescriptorResponse `json:"templates"`
}

func promptTemplatePtr(m map[string]string, key string) *string {
	if value, ok := m[key]; ok {
		v := value
		return &v
	}
	return nil
}

func promptTemplateScopeStrings(def prompttmpl.Definition) []string {
	out := make([]string, 0, len(def.SupportedScopes))
	for _, scope := range def.SupportedScopes {
		out = append(out, string(scope))
	}
	return out
}

func workspacePromptTemplateResponse(ws db.Workspace) PromptTemplateListResponse {
	workspaceOverrides := prompttmpl.ExtractWorkspaceOverridesFromRaw(ws.Settings, "")
	templates := make([]PromptTemplateDescriptorResponse, 0, len(prompttmpl.Definitions()))
	for _, def := range prompttmpl.Definitions() {
		effective := def.DefaultTemplate
		source := "default"
		if value, ok := workspaceOverrides[def.Key]; ok {
			effective = value
			source = "workspace"
		}
		templates = append(templates, PromptTemplateDescriptorResponse{
			Key:                def.Key,
			Title:              def.Title,
			Description:        def.Description,
			SupportedScopes:    promptTemplateScopeStrings(def),
			SupportedVariables: def.SupportedVariables,
			DefaultTemplate:    def.DefaultTemplate,
			WorkspaceOverride:  promptTemplatePtr(workspaceOverrides, def.Key),
			BaseTemplate:       def.DefaultTemplate,
			BaseSource:         "default",
			EffectiveTemplate:  effective,
			EffectiveSource:    source,
		})
	}
	return PromptTemplateListResponse{Templates: templates}
}

func agentPromptTemplateResponse(ws db.Workspace, agent db.Agent) PromptTemplateListResponse {
	workspaceOverrides := prompttmpl.ExtractWorkspaceOverridesFromRaw(ws.Settings, "")
	agentOverrides := prompttmpl.ExtractOverridesFromRaw(agent.RuntimeConfig)
	templates := make([]PromptTemplateDescriptorResponse, 0, len(prompttmpl.Definitions()))
	for _, def := range prompttmpl.Definitions() {
		baseTemplate := def.DefaultTemplate
		baseSource := "default"
		if value, ok := workspaceOverrides[def.Key]; ok {
			baseTemplate = value
			baseSource = "workspace"
		}
		effective := baseTemplate
		effectiveSource := baseSource
		if value, ok := agentOverrides[def.Key]; ok {
			effective = value
			effectiveSource = "agent"
		}
		templates = append(templates, PromptTemplateDescriptorResponse{
			Key:                def.Key,
			Title:              def.Title,
			Description:        def.Description,
			SupportedScopes:    promptTemplateScopeStrings(def),
			SupportedVariables: def.SupportedVariables,
			DefaultTemplate:    def.DefaultTemplate,
			WorkspaceOverride:  promptTemplatePtr(workspaceOverrides, def.Key),
			AgentOverride:      promptTemplatePtr(agentOverrides, def.Key),
			BaseTemplate:       baseTemplate,
			BaseSource:         baseSource,
			EffectiveTemplate:  effective,
			EffectiveSource:    effectiveSource,
		})
	}
	return PromptTemplateListResponse{Templates: templates}
}

func effectivePromptTemplates(ws db.Workspace, agent db.Agent) map[string]string {
	return prompttmpl.EffectiveTemplates(
		prompttmpl.ExtractWorkspaceOverridesFromRaw(ws.Settings, ""),
		prompttmpl.ExtractOverridesFromRaw(agent.RuntimeConfig),
	)
}

func (h *Handler) GetWorkspacePromptTemplates(w http.ResponseWriter, r *http.Request) {
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, workspacePromptTemplateResponse(ws))
}

func (h *Handler) GetAgentPromptTemplates(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), agent.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace prompt templates")
		return
	}
	writeJSON(w, http.StatusOK, agentPromptTemplateResponse(ws, agent))
}
