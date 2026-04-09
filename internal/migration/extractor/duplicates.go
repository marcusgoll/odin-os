package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

func DetectDuplicates(candidates []Candidate) []Candidate {
	grouped := make([]Candidate, len(candidates))
	copy(grouped, candidates)
	for index := range grouped {
		grouped[index].IsPrimary = true
	}

	buckets := make(map[string][]int)
	for index, candidate := range grouped {
		key := string(candidate.Kind) + "|" + candidate.Key
		if candidate.Key == "" {
			key = string(candidate.Kind) + "|" + candidate.ContentHash
		}
		buckets[key] = append(buckets[key], index)
	}

	for _, indexes := range buckets {
		if len(indexes) < 2 {
			continue
		}

		sort.Slice(indexes, func(i int, j int) bool {
			left := grouped[indexes[i]]
			right := grouped[indexes[j]]
			leftScore := candidatePriority(left)
			rightScore := candidatePriority(right)
			if leftScore != rightScore {
				return leftScore < rightScore
			}
			return left.RelativePath < right.RelativePath
		})

		groupID := duplicateGroupID(grouped[indexes[0]])
		for position, candidateIndex := range indexes {
			grouped[candidateIndex].DuplicateGroup = groupID
			grouped[candidateIndex].IsPrimary = position == 0
		}
	}

	return grouped
}

func duplicateGroupID(candidate Candidate) string {
	sum := sha256.Sum256([]byte(string(candidate.Kind) + "|" + candidate.Key + "|" + candidate.ContentHash))
	return "dup-" + hex.EncodeToString(sum[:8])
}

func candidatePriority(candidate Candidate) int {
	switch {
	case hasSignal(candidate.PathSignals, "backup_path"), hasSignal(candidate.PathSignals, "worktree_path"):
		return 3
	case hasSignal(candidate.PathSignals, "agents_root"):
		return 2
	case hasSignal(candidate.PathSignals, "claude_root"):
		return 1
	default:
		return 0
	}
}
