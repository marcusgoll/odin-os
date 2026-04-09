package extractor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Inventory struct {
	SourceRoot  string      `json:"source_root"`
	Candidates  []Candidate `json:"candidates"`
	GeneratedAt string      `json:"generated_at"`
}

func writeInventoryJSON(path string, inventory Inventory) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func writeMarkdownReport(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}

func renderInventoryReport(inventory Inventory) string {
	classificationCounts := make(map[Classification]int)
	for _, candidate := range inventory.Candidates {
		classificationCounts[candidate.Classification]++
	}

	var lines []string
	lines = append(lines, "# Legacy Migration Inventory")
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Source root: `%s`", inventory.SourceRoot))
	lines = append(lines, "")
	lines = append(lines, "## Summary")
	lines = append(lines, "")
	for _, classification := range []Classification{
		ClassificationRewrite,
		ClassificationReferenceOnly,
		ClassificationArchive,
		ClassificationDelete,
		ClassificationMigrateAsIs,
	} {
		count := classificationCounts[classification]
		if count == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s`: %d", classification, count))
	}
	lines = append(lines, "")
	lines = append(lines, "## Candidates")
	lines = append(lines, "")
	for _, candidate := range inventory.Candidates {
		lines = append(lines, fmt.Sprintf("- `%s` `%s` `%s` -> `%s`", candidate.Kind, candidate.Key, candidate.RelativePath, candidate.Classification))
		lines = append(lines, fmt.Sprintf("  rationale: %s", candidate.Rationale))
	}
	return strings.Join(lines, "\n")
}

func renderDuplicateReport(inventory Inventory) string {
	groups := make(map[string][]Candidate)
	for _, candidate := range inventory.Candidates {
		if candidate.DuplicateGroup == "" {
			continue
		}
		groups[candidate.DuplicateGroup] = append(groups[candidate.DuplicateGroup], candidate)
	}

	groupIDs := make([]string, 0, len(groups))
	for groupID := range groups {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Strings(groupIDs)

	lines := []string{"# Likely Duplicates", ""}
	if len(groupIDs) == 0 {
		lines = append(lines, "No duplicate groups were detected.")
		return strings.Join(lines, "\n")
	}

	for _, groupID := range groupIDs {
		lines = append(lines, fmt.Sprintf("## %s", groupID))
		lines = append(lines, "")
		sort.Slice(groups[groupID], func(i int, j int) bool {
			return groups[groupID][i].RelativePath < groups[groupID][j].RelativePath
		})
		for _, candidate := range groups[groupID] {
			primary := ""
			if candidate.IsPrimary {
				primary = " primary"
			}
			lines = append(lines, fmt.Sprintf("- `%s` `%s`%s", candidate.Kind, candidate.RelativePath, primary))
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
