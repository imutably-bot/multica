export type PromptTemplateScope = "workspace" | "agent";
export type PromptTemplateSource = "default" | "workspace" | "agent";

export interface PromptTemplateDescriptor {
  key: string;
  title: string;
  description: string;
  supported_scopes: PromptTemplateScope[];
  supported_variables: string[];
  default_template: string;
  workspace_override?: string | null;
  agent_override?: string | null;
  base_template: string;
  base_source: Exclude<PromptTemplateSource, "agent">;
  effective_template: string;
  effective_source: PromptTemplateSource;
}

export interface PromptTemplateListResponse {
  templates: PromptTemplateDescriptor[];
}
