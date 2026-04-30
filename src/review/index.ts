export interface ReviewFinding {
  readonly severity: "low" | "medium" | "high";
  readonly message: string;
}

export interface ReviewSummary {
  readonly findings: readonly ReviewFinding[];
  readonly humanReviewRequired: true;
}

export function createEmptyReviewSummary(): ReviewSummary {
  return {
    findings: [],
    humanReviewRequired: true
  };
}
