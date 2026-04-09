package extractor

import "strings"

func Classify(candidates []Candidate) []Candidate {
	classified := make([]Candidate, len(candidates))
	for index, candidate := range candidates {
		classified[index] = candidate
		classified[index].Classification, classified[index].Rationale = classifyCandidate(candidate)
	}
	return classified
}

func classifyCandidate(candidate Candidate) (Classification, string) {
	if candidate.Kind == KindUnknown {
		return ClassificationDelete, "unknown or non-canonical legacy asset"
	}
	if hasSignal(candidate.PathSignals, "backup_path") || hasSignal(candidate.PathSignals, "worktree_path") {
		return ClassificationArchive, "backup or worktree path is not canonical"
	}
	if strings.Contains(candidate.RelativePath, ".cache/") || strings.Contains(candidate.RelativePath, ".git/") {
		return ClassificationDelete, "generated or cached path is not migratable"
	}

	switch candidate.Kind {
	case KindArchitectureDoc:
		return ClassificationReferenceOnly, "architecture material should inform design but not be promoted directly"
	case KindSkill, KindAgent, KindWorkflow, KindPrompt, KindOperationalPlaybook:
		return ClassificationRewrite, "legacy asset needs normalization into the new contract"
	default:
		return ClassificationDelete, "unsupported legacy asset kind"
	}
}

func hasSignal(signals []string, want string) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}
