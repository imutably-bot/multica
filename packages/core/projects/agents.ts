import { useMutation, useQueryClient, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

export function agentProjectsOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.agents(wsId), agentId, "projects"],
    queryFn: async () => {
      const resp = await api.listAgentProjects(agentId);
      return resp.projects;
    },
    enabled: !!wsId && !!agentId,
  });
}

export function useSetProjectAgents(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentIds: string[]) => api.setProjectAgents(projectId, agentIds),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId)],
      });
    },
  });
}

export function useAddProjectAgent(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => api.addAgentsToProject(projectId, [agentId]),
    onSuccess: (_, agentId) => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
      });
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), agentId, "projects"],
      });
    },
  });
}

export function useRemoveProjectAgent(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => api.removeAgentsFromProject(projectId, [agentId]),
    onSuccess: (_, agentId) => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
      });
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), agentId, "projects"],
      });
    },
  });
}
