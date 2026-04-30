export interface WorkspaceRequest {
  readonly workItemId: string;
  readonly projectKey: string;
}

export interface WorkspaceLease {
  readonly branchName: string;
  readonly path: string;
}

export interface WorkspaceManager {
  prepareWorkspace(request: WorkspaceRequest): Promise<WorkspaceLease>;
}

export function createWorkspaceManager(options: { readonly root: string }): WorkspaceManager {
  return {
    async prepareWorkspace(request) {
      return {
        branchName: `odin/${request.projectKey}/work-item-${request.workItemId}/try-1`,
        path: `${options.root}/${request.projectKey}/${request.workItemId}`
      };
    }
  };
}
