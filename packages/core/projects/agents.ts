import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

export function useSetProjectAgents(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentIds: string[]) => api.setProjectAgents(projectId, agentIds),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
      });
    },
  });
}

export function useAddProjectAgent(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => api.addAgentsToProject(projectId, [agentId]),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
      });
    },
  });
}

export function useRemoveProjectAgent(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => api.removeAgentsFromProject(projectId, [agentId]),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
      });
    },
  });
}
