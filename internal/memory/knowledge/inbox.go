package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s Service) InboxPath() string {
	path := filepath.Join(s.RuntimeRoot, "knowledge", "inbox")
	_ = os.MkdirAll(path, 0o755)
	return path
}

func (s Service) ListInbox(ctx context.Context) ([]InboxEntry, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	inboxPath := s.InboxPath()
	entries, err := os.ReadDir(inboxPath)
	if err != nil {
		return nil, err
	}

	result := make([]InboxEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeType != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(inboxPath, entry.Name())
		inboxEntry := InboxEntry{
			Name:      entry.Name(),
			Path:      path,
			SizeBytes: info.Size(),
		}
		sourceClass, err := inferSourceClass(path)
		if err != nil {
			inboxEntry.RejectedReason = err.Error()
		} else {
			inboxEntry.SourceClass = sourceClass
			inboxEntry.Supported = true
		}
		result = append(result, inboxEntry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s Service) IngestInbox(ctx context.Context, params IngestInboxParams) (IngestResult, error) {
	if params.All {
		entries, err := s.ListInbox(ctx)
		if err != nil {
			return IngestResult{}, err
		}
		if len(entries) == 0 {
			return IngestResult{}, fmt.Errorf("knowledge inbox is empty")
		}
		var last IngestResult
		for _, entry := range entries {
			next := params
			next.All = false
			next.Name = entry.Name
			next.Key = ""
			next.Title = ""
			result, err := s.IngestInbox(ctx, next)
			if err != nil {
				return IngestResult{}, err
			}
			last = result
		}
		return last, nil
	}

	inboxPath := s.InboxPath()
	name, err := cleanInboxName(params.Name)
	if err != nil {
		return IngestResult{}, err
	}
	sourcePath := filepath.Join(inboxPath, name)
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return IngestResult{}, err
	}
	if info.Mode()&os.ModeType != 0 || !info.Mode().IsRegular() {
		return IngestResult{}, fmt.Errorf("knowledge inbox entry %q is not a regular file", name)
	}

	key := strings.TrimSpace(params.Key)
	if key == "" {
		key = inboxKeyFromName(name)
	}
	title := strings.TrimSpace(params.Title)
	if title == "" {
		title = inboxTitleFromKey(key)
	}
	ingestParams := IngestParams{
		Path:           sourcePath,
		Key:            key,
		Title:          title,
		Scope:          valueOrDefault(params.Scope, "global"),
		ScopeKey:       valueOrDefault(params.ScopeKey, "global"),
		Restricted:     params.Restricted,
		SourceKind:     valueOrDefault(params.SourceKind, "manual"),
		SourceClass:    params.SourceClass,
		RefreshPolicy:  params.RefreshPolicy,
		CitationPolicy: params.CitationPolicy,
		Topics:         params.Topics,
		Entities:       params.Entities,
		RelatedSources: params.RelatedSources,
		AppliesTo:      params.AppliesTo,
	}

	result, ingestErr := s.Ingest(ctx, ingestParams)
	if ingestErr != nil {
		if rejectErr := s.rejectInboxFile(name, sourcePath, ingestErr.Error()); rejectErr != nil {
			return IngestResult{}, fmt.Errorf("%w; failed to reject inbox file: %v", ingestErr, rejectErr)
		}
		return IngestResult{}, ingestErr
	}
	if result.Extraction.FailureCode != "" {
		reason := result.Extraction.FailureCode
		if strings.TrimSpace(result.Extraction.FailureSummary) != "" {
			reason += ": " + result.Extraction.FailureSummary
		}
		if err := s.rejectInboxFile(name, sourcePath, reason); err != nil {
			return IngestResult{}, err
		}
		return result, nil
	}
	if err := s.importInboxFile(name, sourcePath); err != nil {
		return IngestResult{}, err
	}
	return result, nil
}

func cleanInboxName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("knowledge inbox file name is required")
	}
	if filepath.Base(name) != name || name == "." || name == ".." {
		return "", fmt.Errorf("knowledge inbox file name %q must not include path separators", name)
	}
	return name, nil
}

func (s Service) importInboxFile(name string, sourcePath string) error {
	dest := filepath.Join(s.RuntimeRoot, "knowledge", "imported", name)
	return moveInboxFile(sourcePath, dest)
}

func (s Service) rejectInboxFile(name string, sourcePath string, reason string) error {
	dest := filepath.Join(s.RuntimeRoot, "knowledge", "rejected", name)
	if err := moveInboxFile(sourcePath, dest); err != nil {
		return err
	}
	reasonPath := dest + ".reason.txt"
	return os.WriteFile(reasonPath, []byte(strings.TrimSpace(reason)+"\n"), 0o644)
}

func moveInboxFile(sourcePath string, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("knowledge inbox destination already exists: %s", destPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(sourcePath, destPath)
}

func inboxKeyFromName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return slugifyKnowledgeAnchor(base)
}

func inboxTitleFromKey(key string) string {
	parts := strings.Split(key, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
