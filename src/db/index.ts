import type { WorkItem } from "../orchestrator/types.js";

export interface RuntimeStore {
  listOpenWorkItems(): Promise<readonly WorkItem[]>;
  saveWorkItem(workItem: WorkItem): Promise<void>;
}

export function createPlaceholderStore(): RuntimeStore {
  const workItems = new Map<string, WorkItem>();

  return {
    async listOpenWorkItems() {
      return [...workItems.values()];
    },
    async saveWorkItem(workItem) {
      workItems.set(workItem.id, workItem);
    }
  };
}
