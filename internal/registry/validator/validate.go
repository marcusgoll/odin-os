package validator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"odin-os/internal/registry"
	"odin-os/internal/skills/permissionspec"
)

const (
	supportedSkillHandlerType = "command"
	maxSkillTimeoutSeconds    = 300
)

func ValidateDocuments(documents []registry.ParsedDocument) []registry.Diagnostic {
	var diagnostics []registry.Diagnostic
	keys := make(map[string][]registry.ParsedDocument)

	for _, document := range documents {
		diagnostics = append(diagnostics, validateDocument(document)...)
		if document.Frontmatter.Key != "" {
			keys[document.Frontmatter.Key] = append(keys[document.Frontmatter.Key], document)
		}
	}

	for key, duplicates := range keys {
		if len(duplicates) < 2 {
			continue
		}

		for _, duplicate := range duplicates {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(
				duplicate.Source.Path,
				"duplicate_key",
				fmt.Sprintf("registry key %q is declared more than once", key),
			))
		}
	}

	registry.SortDiagnostics(diagnostics)
	return diagnostics
}

func validateDocument(document registry.ParsedDocument) []registry.Diagnostic {
	var diagnostics []registry.Diagnostic

	if document.Source.ExpectedKind == registry.KindUnknown {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(document.Source.Path, "invalid_path_kind", "registry file must live under agents, skills, workflows, or commands"))
	}

	if !document.Frontmatter.Kind.Valid() {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(document.Source.Path, "invalid_kind", "frontmatter kind must be one of agent, skill, workflow, or command"))
	} else if document.Source.ExpectedKind != registry.KindUnknown && document.Frontmatter.Kind != document.Source.ExpectedKind {
		diagnostics = append(diagnostics, registry.ErrorDiagnostic(
			document.Source.Path,
			"kind_mismatch",
			fmt.Sprintf("frontmatter kind %q does not match path kind %q", document.Frontmatter.Kind, document.Source.ExpectedKind),
		))
	}

	requireString(document.Source.Path, &diagnostics, "missing_field", "key", document.Frontmatter.Key)
	requireString(document.Source.Path, &diagnostics, "missing_field", "title", document.Frontmatter.Title)
	requireString(document.Source.Path, &diagnostics, "missing_field", "summary", document.Frontmatter.Summary)
	if strings.TrimSpace(document.Frontmatter.Key) != "" {
		if err := registry.ValidateKey(document.Frontmatter.Key); err != nil {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(
				document.Source.Path,
				"invalid_key",
				err.Error(),
			))
		}
	}

	switch document.Frontmatter.Kind {
	case registry.KindAgent:
		requireString(document.Source.Path, &diagnostics, "missing_field", "role", document.Frontmatter.Role)
		requireList(document.Source.Path, &diagnostics, "missing_field", "scopes", document.Frontmatter.Scopes)
		requireList(document.Source.Path, &diagnostics, "missing_field", "tools", document.Frontmatter.Tools)
	case registry.KindSkill:
		requireString(document.Source.Path, &diagnostics, "missing_field", "version", document.Frontmatter.Version)
		requireBool(document.Source.Path, &diagnostics, "missing_field", "enabled", document.Frontmatter.Enabled)
		requireString(document.Source.Path, &diagnostics, "missing_field", "strictness", document.Frontmatter.Strictness)
		requireList(document.Source.Path, &diagnostics, "missing_field", "applies_to", document.Frontmatter.AppliesTo)
		requireList(document.Source.Path, &diagnostics, "missing_field", "scopes", document.Frontmatter.Scopes)
		requireList(document.Source.Path, &diagnostics, "missing_field", "permissions", document.Frontmatter.Permissions)
		requireString(document.Source.Path, &diagnostics, "missing_field", "handler_type", document.Frontmatter.HandlerType)
		requireString(document.Source.Path, &diagnostics, "missing_field", "handler_ref", document.Frontmatter.HandlerRef)
		requirePositiveInt(document.Source.Path, &diagnostics, "missing_field", "timeout_seconds", document.Frontmatter.TimeoutSeconds)
		requireSchema(document.Source.Path, &diagnostics, "missing_field", "input_schema", "invalid_schema", document.Frontmatter.InputSchema)
		requireSchema(document.Source.Path, &diagnostics, "missing_field", "output_schema", "invalid_schema", document.Frontmatter.OutputSchema)
		validateSkillRuntimeContract(document.Source.Path, &diagnostics, document.Frontmatter)
		validateSkillHandlerRef(document.Source, &diagnostics, document.Frontmatter.HandlerRef)
		validateSkillPermissions(document.Source.Path, &diagnostics, document.Frontmatter.Permissions)
	case registry.KindWorkflow:
		requireString(document.Source.Path, &diagnostics, "missing_field", "entrypoint", document.Frontmatter.Entrypoint)
		requireList(document.Source.Path, &diagnostics, "missing_field", "composes", document.Frontmatter.Composes)
	case registry.KindCommand:
		requireString(document.Source.Path, &diagnostics, "missing_field", "command", document.Frontmatter.Command)
		requireList(document.Source.Path, &diagnostics, "missing_field", "scopes", document.Frontmatter.Scopes)
	}

	for _, section := range registry.RequiredSections {
		value, ok := document.Sections[section]
		if !ok || strings.TrimSpace(value) == "" {
			diagnostics = append(diagnostics, registry.ErrorDiagnostic(
				document.Source.Path,
				"missing_section",
				fmt.Sprintf("required section %q is missing or empty", section),
			))
		}
	}

	return diagnostics
}

func requireString(path string, diagnostics *[]registry.Diagnostic, code string, field string, value string) {
	if strings.TrimSpace(value) == "" {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireList(path string, diagnostics *[]registry.Diagnostic, code string, field string, values []string) {
	if len(values) == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireBool(path string, diagnostics *[]registry.Diagnostic, code string, field string, value *bool) {
	if value == nil {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requirePositiveInt(path string, diagnostics *[]registry.Diagnostic, code string, field string, value int) {
	if value <= 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, code, "required frontmatter field "+field+" is missing"))
	}
}

func requireSchema(path string, diagnostics *[]registry.Diagnostic, missingCode string, field string, invalidCode string, schema map[string]any) {
	if len(schema) == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, missingCode, "required frontmatter field "+field+" is missing"))
		return
	}
	if !schemaIsObject(schema) {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(path, invalidCode, "frontmatter field "+field+" must declare an object schema"))
	}
}

func validateSkillRuntimeContract(path string, diagnostics *[]registry.Diagnostic, frontmatter registry.Frontmatter) {
	if strings.TrimSpace(frontmatter.HandlerType) != "" && frontmatter.HandlerType != supportedSkillHandlerType {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			path,
			"invalid_handler_type",
			fmt.Sprintf("skill handler_type must be %q", supportedSkillHandlerType),
		))
	}

	if frontmatter.TimeoutSeconds > maxSkillTimeoutSeconds {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			path,
			"invalid_timeout",
			fmt.Sprintf("skill timeout_seconds must be between 1 and %d", maxSkillTimeoutSeconds),
		))
	}
}

func validateSkillPermissions(path string, diagnostics *[]registry.Diagnostic, permissions []string) {
	if len(permissions) == 0 {
		return
	}

	for _, permission := range permissions {
		if _, err := permissionspec.Parse(permission); err != nil {
			*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
				path,
				"invalid_permission",
				fmt.Sprintf("invalid permission %q", permission),
			))
		}
	}
}

func validateSkillHandlerRef(source registry.SourceFile, diagnostics *[]registry.Diagnostic, handlerRef string) {
	cleaned := filepath.Clean(strings.TrimSpace(handlerRef))
	if cleaned == "" {
		return
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q must stay within the repo", handlerRef),
		))
		return
	}
	normalized := filepath.ToSlash(cleaned)
	if normalized != registry.SkillHandlerRoot && !strings.HasPrefix(normalized, registry.SkillHandlerRoot+"/") {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q must stay under %s", handlerRef, registry.SkillHandlerRoot),
		))
		return
	}

	repoRoot, err := sourceRepoRoot(source)
	if err != nil {
		return
	}
	handlerPath := filepath.Join(repoRoot, cleaned)
	info, err := os.Lstat(handlerPath)
	if err != nil {
		if os.IsNotExist(err) {
			*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
				source.Path,
				"invalid_handler_ref",
				fmt.Sprintf("skill handler_ref %q must point to an existing executable file", handlerRef),
			))
			return
		}
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q could not be inspected: %v", handlerRef, err),
		))
		return
	}
	_ = info

	resolvedPath, err := filepath.EvalSymlinks(handlerPath)
	if err != nil {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q could not be resolved: %v", handlerRef, err),
		))
		return
	}
	if err := validateResolvedSkillHandlerPath(repoRoot, resolvedPath); err != nil {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q %s", handlerRef, err.Error()),
		))
		return
	}

	resolvedInfo, err := os.Stat(resolvedPath)
	if err != nil {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q could not be inspected: %v", handlerRef, err),
		))
		return
	}
	if resolvedInfo.IsDir() {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q must point to an existing executable file", handlerRef),
		))
		return
	}
	if resolvedInfo.Mode()&0o111 == 0 {
		*diagnostics = append(*diagnostics, registry.ErrorDiagnostic(
			source.Path,
			"invalid_handler_ref",
			fmt.Sprintf("skill handler_ref %q must point to an existing executable file", handlerRef),
		))
	}
}

func sourceRepoRoot(source registry.SourceFile) (string, error) {
	relativeRegistryPath := filepath.Join("registry", filepath.FromSlash(source.RelativePath))
	steps := strings.Split(filepath.ToSlash(relativeRegistryPath), "/")
	root := filepath.Clean(source.Path)
	for range steps {
		root = filepath.Dir(root)
	}
	return root, nil
}

func validateResolvedSkillHandlerPath(repoRoot string, resolvedPath string) error {
	relativeToRepo, err := filepath.Rel(repoRoot, resolvedPath)
	if err != nil {
		return err
	}
	if relativeToRepo == ".." || strings.HasPrefix(relativeToRepo, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must stay within the repo")
	}

	allowedRoot := filepath.Join(repoRoot, registry.SkillHandlerRoot)
	relativeToAllowedRoot, err := filepath.Rel(allowedRoot, resolvedPath)
	if err != nil {
		return err
	}
	if relativeToAllowedRoot == ".." || strings.HasPrefix(relativeToAllowedRoot, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must resolve under %s", registry.SkillHandlerRoot)
	}
	return nil
}

func schemaIsObject(schema map[string]any) bool {
	typeValue, ok := schema["type"]
	if !ok {
		return false
	}

	typeString, ok := typeValue.(string)
	if !ok {
		return false
	}

	return strings.TrimSpace(typeString) == "object"
}
