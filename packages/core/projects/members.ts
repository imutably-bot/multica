import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { projectKeys } from "./queries";
import type {
  AddProjectMemberRequest,
  ListProjectMembersResponse,
} from "../types";

export const projectMemberKeys = {
  list: (wsId: string, projectId: string) =>
    [...projectKeys.detail(wsId, projectId), "members"] as const,
};

export function projectMembersOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectMemberKeys.list(wsId, projectId),
    queryFn: () => api.listProjectMembers(projectId),
  });
}

export function useAddProjectMember(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: AddProjectMemberRequest) =>
      api.addProjectMember(projectId, data),
    onSuccess: (next) => {
      qc.setQueryData<ListProjectMembersResponse>(
        projectMemberKeys.list(wsId, projectId),
        next,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectMemberKeys.list(wsId, projectId),
      });
    },
  });
}

export function useRemoveProjectMember(wsId: string, projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.removeProjectMember(projectId, userId),
    onSuccess: (next) => {
      qc.setQueryData<ListProjectMembersResponse>(
        projectMemberKeys.list(wsId, projectId),
        next,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({
        queryKey: projectMemberKeys.list(wsId, projectId),
      });
    },
  });
}
