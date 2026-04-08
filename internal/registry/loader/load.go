package loader

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"odin-os/internal/registry"
	"odin-os/internal/registry/compiler"
	"odin-os/internal/registry/parser"
)

func ScanDir(root string) ([]registry.SourceFile, error) {
	var files []registry.SourceFile

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)

		firstSegment := relativePath
		if index := strings.Index(firstSegment, "/"); index >= 0 {
			firstSegment = firstSegment[:index]
		}

		files = append(files, registry.SourceFile{
			Path:         path,
			RelativePath: relativePath,
			ExpectedKind: registry.KindFromDirectory(firstSegment),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i int, j int) bool {
		return files[i].RelativePath < files[j].RelativePath
	})

	return files, nil
}

func LoadDir(root string) (registry.Snapshot, error) {
	files, err := ScanDir(root)
	if err != nil {
		return registry.Snapshot{}, err
	}

	var documents []registry.ParsedDocument
	var diagnostics []registry.Diagnostic

	for _, file := range files {
		content, err := os.ReadFile(file.Path)
		if err != nil {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(file.Path, "read_error", "registry file could not be read: "+err.Error()))
			continue
		}

		document, parseDiagnostics := parser.ParseSource(file, content)
		documents = append(documents, document)
		diagnostics = append(diagnostics, parseDiagnostics...)
	}

	return compiler.Compile(documents, diagnostics), nil
}
