package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

const ledongthucPDFExtractorVersion = "v0.0.0-20250511090121-5959a4027728"

type extractionResult struct {
	Text               string
	NormalizedMarkdown string
	Anchors            []string
	Pages              []extractedPage
	ExtractorName      string
	ExtractorVersion   string
	FailureCode        string
	FailureSummary     string
}

type extractedPage struct {
	Number int64
	Text   string
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
	case ".pdf":
		return SourceClassMachineReadablePDF, nil
	default:
		return "", fmt.Errorf("unsupported source class for %q", sourcePath)
	}
}

func validateTask2SourceClass(sourceClass SourceClass) error {
	switch sourceClass {
	case SourceClassMarkdown, SourceClassText, SourceClassMachineReadablePDF:
		return nil
	default:
		return fmt.Errorf("unsupported source class %q", sourceClass)
	}
}

func extractSource(sourcePath string, sourceClass SourceClass) (extractionResult, error) {
	switch sourceClass {
	case SourceClassMarkdown:
		text, err := readNormalizedTextFile(sourcePath)
		if err != nil {
			return extractionResult{}, err
		}
		normalized := strings.TrimSpace(text) + "\n"
		return extractionResult{
			Text:               strings.TrimSpace(stripMarkdownMarkers(text)),
			NormalizedMarkdown: normalized,
			Anchors:            markdownAnchors(normalized),
			ExtractorName:      "markdown",
			ExtractorVersion:   "v1",
		}, nil
	case SourceClassText:
		text, err := readNormalizedTextFile(sourcePath)
		if err != nil {
			return extractionResult{}, err
		}
		extracted := strings.TrimSpace(text)
		return extractionResult{
			Text:               extracted,
			NormalizedMarkdown: textToMarkdown(extracted),
			Anchors:            textAnchors(extracted),
			ExtractorName:      "plain_text",
			ExtractorVersion:   "v1",
		}, nil
	case SourceClassMachineReadablePDF:
		return extractMachineReadablePDF(sourcePath), nil
	default:
		return extractionResult{
			FailureCode:    "unsupported_source_class",
			FailureSummary: fmt.Sprintf("unsupported source class %q", sourceClass),
		}, fmt.Errorf("unsupported source class %q", sourceClass)
	}
}

func readNormalizedTextFile(sourcePath string) (string, error) {
	bytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(bytes), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, nil
}

func extractMachineReadablePDF(sourcePath string) extractionResult {
	result := extractionResult{
		ExtractorName:    "ledongthuc_pdf",
		ExtractorVersion: ledongthucPDFExtractorVersion,
	}
	file, reader, err := pdf.Open(sourcePath)
	if err != nil {
		result.FailureCode = "pdf_unreadable"
		result.FailureSummary = err.Error()
		return result
	}
	defer file.Close()

	pageCount := reader.NumPage()
	if pageCount <= 0 {
		result.FailureCode = "pdf_unreadable"
		result.FailureSummary = "PDF has no readable pages"
		return result
	}

	fonts := make(map[string]*pdf.Font)
	pages := make([]extractedPage, 0, pageCount)
	allText := make([]string, 0, pageCount)
	for pageNumber := 1; pageNumber <= pageCount; pageNumber++ {
		page := reader.Page(pageNumber)
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, err := page.GetPlainText(fonts)
		if err != nil {
			result.FailureCode = "pdf_unreadable"
			result.FailureSummary = err.Error()
			return result
		}
		text = normalizeExtractedText(text)
		if text == "" {
			continue
		}
		pages = append(pages, extractedPage{Number: int64(pageNumber), Text: text})
		allText = append(allText, text)
	}

	result.Text = strings.TrimSpace(strings.Join(allText, "\n\n"))
	result.Pages = pages
	result.NormalizedMarkdown = pdfPagesToMarkdown(pages)
	result.Anchors = pdfPageAnchors(pages)
	if !hasMeaningfulText(result.Text) {
		result.FailureCode = "ocr_required"
		result.FailureSummary = "PDF contains no meaningful machine-readable text"
	}
	return result
}

func normalizeExtractedText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func pdfPagesToMarkdown(pages []extractedPage) string {
	if len(pages) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, page := range pages {
		if strings.TrimSpace(page.Text) == "" {
			continue
		}
		fmt.Fprintf(&builder, "## Page %d\n\n%s\n\n", page.Number, strings.TrimSpace(page.Text))
	}
	return strings.TrimSpace(builder.String()) + "\n"
}

func pdfPageAnchors(pages []extractedPage) []string {
	anchors := make([]string, 0, len(pages))
	for _, page := range pages {
		anchors = append(anchors, fmt.Sprintf("page:%d", page.Number))
	}
	return anchors
}

func hasMeaningfulText(text string) bool {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			count++
			if count >= 3 {
				return true
			}
		}
	}
	return false
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
