export interface GithubIssue {
  readonly id: string;
  readonly number: number;
  readonly title: string;
  readonly labels: readonly string[];
  readonly url: string;
}

export interface IssueTracker {
  listEligibleIssues(): Promise<readonly GithubIssue[]>;
}

export function createGithubIssueTracker(_options: { readonly dryRun: boolean }): IssueTracker {
  return {
    async listEligibleIssues() {
      return [];
    }
  };
}
