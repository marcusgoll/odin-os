package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type extractionResult struct {
	Text               string
	NormalizedMarkdown string
	Anchors            []string
	ExtractorName      string
	ExtractorVersion   string
	FailureCode        string
	FailureSummary     string
}

func (r extractionResult) Extractor() string {
	return r.ExtractorName + ":" + r.ExtractorVersion
}

func inferSourceClass(sourcePath string) (SourceClass, error) {
	switch strings.ToLower(filepath.Ext(sourcePath)) {
	case ".md", ".markdown":
		return SourceClassMarkdown, nil
	case ".txt", ".text":
		return SourceClassText, nil
	default:
		return "", fmt.Errorf("unsupported source class for %q", sourcePath)
	}
}

func validateTask2SourceClass(sourceClass SourceClass) error {
	switch sourceClass {
	case SourceClassMarkdown, SourceClassText:
		return nil
	default:
		return fmt.Errorf("unsupported source class %q", sourceClass)
	}
}

func extractSource(sourcePath string, sourceClass SourceClass) (extractionResult, error) {
	bytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return extractionResult{}, err
	}
	text := strings.ReplaceAll(string(bytes), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	switch sourceClass {
	case SourceClassMarkdown:
		normalized := strings.TrimSpace(text) + "\n"
		return extractionResult{
			Text:               strings.TrimSpace(stripMarkdownMarkers(text)),
			NormalizedMarkdown: normalized,
			Anchors:            markdownAnchors(normalized),
			ExtractorName:      "markdown",
			ExtractorVersion:   "v1",
		}, nil
	case SourceClassText:
		extracted := strings.TrimSpace(text)
		return extractionResult{
			Text:               extracted,
			NormalizedMarkdown: textToMarkdown(extracted),
			Anchors:            textAnchors(extracted),
			ExtractorName:      "plain_text",
			ExtractorVersion:   "v1",
		}, nil
	default:
		return extractionResult{
			FailureCode:    "unsupported_source_class",
			FailureSummary: fmt.Sprintf("unsupported source class %q", sourceClass),
		}, fmt.Errorf("unsupported source class %q", sourceClass)
	}
}

func stripMarkdownMarkers(markdown string) string {
	lines := strings.Split(markdown, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		}
	}
	return strings.Join(lines, "\n")
}

func textToMarkdown(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	paragraphs := strings.Split(strings.TrimSpace(text), "\n\n")
	for i, paragraph := range paragraphs {
		lines := strings.Split(paragraph, "\n")
		for j, line := range lines {
			lines[j] = strings.TrimRight(line, " \t")
		}
		paragraphs[i] = strings.Join(lines, "\n")
	}
	return strings.Join(paragraphs, "\n\n") + "\n"
}

var headingAnchorPattern = regexp.MustCompile(`[^a-z0-9]+`)

func markdownAnchors(markdown string) []string {
	var anchors []string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if title == "" {
			continue
		}
		slug := strings.Trim(headingAnchorPattern.ReplaceAllString(strings.ToLower(title), "-"), "-")
		if slug != "" {
			anchors = append(anchors, "section:"+slug)
		}
	}
	return anchors
}

func textAnchors(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []string{"section:start"}
}
