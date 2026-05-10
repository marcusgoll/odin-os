package registry

import (
	"sort"
	"strings"
)

const NormalizedAPIVersion = "odin/v1"

type Kind string

const (
	KindUnknown  Kind = ""
	KindAgent    Kind = "agent"
	KindSkill    Kind = "skill"
	KindWorkflow Kind = "workflow"
	KindCommand  Kind = "command"
)

const (
	SectionPurpose         = "Purpose"
	SectionWhenToUse       = "When to Use"
	SectionInputs          = "Inputs"
	SectionProcedure       = "Procedure"
	SectionOutputs         = "Outputs"
	SectionConstraints     = "Constraints"
	SectionSuccessCriteria = "Success Criteria"
)

var RequiredSections = []string{
	SectionPurpose,
	SectionWhenToUse,
	SectionInputs,
	SectionProcedure,
	SectionOutputs,
	SectionConstraints,
	SectionSuccessCriteria,
}

type SourceFile struct {
	Path         string
	RelativePath string
	ExpectedKind Kind
}

type SourceInfo struct {
	Path         string
	RelativePath string
}

type Manifest struct {
	APIVersion     string            `yaml:"apiVersion"`
	Kind           Kind              `yaml:"kind"`
	Name           string            `yaml:"name"`
	Version        string            `yaml:"version"`
	Availability   Availability      `yaml:"availability"`
	Permissions    []string          `yaml:"permissions"`
	InputSchema    SchemaRef         `yaml:"inputSchema"`
	OutputSchema   SchemaRef         `yaml:"outputSchema"`
	Dependencies   []DependencyRef   `yaml:"dependencies"`
	Execution      ExecutionPolicy   `yaml:"execution"`
	Implementation ImplementationRef `yaml:"implementation"`
}

type Availability struct {
	Scope string `yaml:"scope"`
	Mode  string `yaml:"mode,omitempty"`
}

type SchemaRef struct {
	Ref  string `yaml:"ref,omitempty"`
	Type string `yaml:"type,omitempty"`
}

type DependencyRef struct {
	Kind    Kind   `yaml:"kind,omitempty"`
	Name    string `yaml:"name,omitempty"`
	Version string `yaml:"version,omitempty"`
}

type ExecutionPolicy struct {
	Mode    string `yaml:"mode,omitempty"`
	Timeout string `yaml:"timeout,omitempty"`
}

type ImplementationRef struct {
	Kind string `yaml:"kind,omitempty"`
	Ref  string `yaml:"ref,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type Frontmatter struct {
	APIVersion     string            `yaml:"apiVersion"`
	Kind           Kind              `yaml:"kind"`
	Name           string            `yaml:"name"`
	Version        string            `yaml:"version"`
	Availability   Availability      `yaml:"availability"`
	Permissions    []string          `yaml:"permissions"`
	InputSchema    SchemaRef         `yaml:"inputSchema"`
	OutputSchema   SchemaRef         `yaml:"outputSchema"`
	Dependencies   []DependencyRef   `yaml:"dependencies"`
	Execution      ExecutionPolicy   `yaml:"execution"`
	Implementation ImplementationRef `yaml:"implementation"`

	Key                string            `yaml:"key"`
	Title              string            `yaml:"title"`
	Summary            string            `yaml:"summary"`
	Status             string            `yaml:"status"`
	Enabled            *bool             `yaml:"enabled"`
	Tags               []string          `yaml:"tags"`
	Owners             []string          `yaml:"owners"`
	Role               string            `yaml:"role"`
	Scopes             []string          `yaml:"scopes"`
	Tools              []string          `yaml:"tools"`
	Delegation         DelegationProfile `yaml:"delegation"`
	Strictness         string            `yaml:"strictness"`
	AppliesTo          []string          `yaml:"applies_to"`
	LegacyInputSchema  map[string]any    `yaml:"input_schema"`
	LegacyOutputSchema map[string]any    `yaml:"output_schema"`
	HandlerType        string            `yaml:"handler_type"`
	HandlerRef         string            `yaml:"handler_ref"`
	TimeoutSeconds     int               `yaml:"timeout_seconds"`
	Entrypoint         string            `yaml:"entrypoint"`
	Composes           []string          `yaml:"composes"`
	Command            string            `yaml:"command"`
	Aliases            []string          `yaml:"aliases"`
}

type ParsedDocument struct {
	Source       SourceFile
	Frontmatter  Frontmatter
	Body         string
	Sections     map[string]string
	SectionOrder []string
}

type DelegationProfile struct {
	Enabled         bool                     `yaml:"enabled"`
	OperatorSurface string                   `yaml:"operator_surface"`
	Inputs          DelegationInputs         `yaml:"inputs"`
	ConvergenceMode string                   `yaml:"convergence_mode"`
	Children        []DelegationChildProfile `yaml:"children"`
}

type DelegationInputs struct {
	Required []string `yaml:"required"`
	Optional []string `yaml:"optional"`
}

type DelegationChildProfile struct {
	DelegationKey         string   `yaml:"delegation_key"`
	Role                  string   `yaml:"role"`
	Wave                  int      `yaml:"wave"`
	ActionClass           string   `yaml:"action_class"`
	ActionKeyTemplate     string   `yaml:"action_key_template"`
	MutationModeSource    string   `yaml:"mutation_mode_source"`
	ConvergenceMode       string   `yaml:"convergence_mode"`
	ArtifactTarget        string   `yaml:"artifact_target"`
	Executor              string   `yaml:"executor"`
	SkillKey              string   `yaml:"skill_key"`
	RequestedTools        []string `yaml:"requested_tools"`
	RequestedMemoryScopes []string `yaml:"requested_memory_scopes"`
}

type DiagnosticSeverity string

const SeverityError DiagnosticSeverity = "error"

type Diagnostic struct {
	Severity DiagnosticSeverity
	Code     string
	Path     string
	Message  string
}

type Item struct {
	APIVersion     string
	Kind           Kind
	Name           string
	Version        string
	Availability   Availability
	Permissions    []string
	InputSchema    SchemaRef
	OutputSchema   SchemaRef
	Dependencies   []DependencyRef
	Execution      ExecutionPolicy
	Implementation ImplementationRef

	Key                string
	Title              string
	Summary            string
	Status             string
	Enabled            bool
	Tags               []string
	Owners             []string
	Role               string
	Scopes             []string
	Tools              []string
	Delegation         DelegationProfile
	Strictness         string
	AppliesTo          []string
	LegacyInputSchema  map[string]any
	LegacyOutputSchema map[string]any
	HandlerType        string
	HandlerRef         string
	TimeoutSeconds     int
	Entrypoint         string
	Composes           []string
	Command            string
	Aliases            []string
	Sections           map[string]string
	Source             SourceInfo
}

type Snapshot struct {
	Items       []Item
	ByKey       map[string]Item
	ByKind      map[Kind][]Item
	Diagnostics []Diagnostic
}

func (kind Kind) Valid() bool {
	switch kind {
	case KindAgent, KindSkill, KindWorkflow, KindCommand:
		return true
	default:
		return false
	}
}

func (kind Kind) ValidDependencyKind() bool {
	switch kind {
	case KindAgent, KindSkill, KindCommand, Kind("tool"):
		return true
	default:
		return false
	}
}

func (kind Kind) IsInvokable() bool {
	switch kind {
	case KindSkill, KindWorkflow, KindCommand:
		return true
	default:
		return false
	}
}

func (frontmatter Frontmatter) UsesNormalizedManifest() bool {
	return strings.TrimSpace(frontmatter.APIVersion) == NormalizedAPIVersion
}

func (frontmatter Frontmatter) HasUnsupportedAPIVersion() bool {
	apiVersion := strings.TrimSpace(frontmatter.APIVersion)
	return apiVersion != "" && apiVersion != NormalizedAPIVersion
}

func KindFromDirectory(name string) Kind {
	switch strings.ToLower(name) {
	case "agents":
		return KindAgent
	case "skills":
		return KindSkill
	case "workflows":
		return KindWorkflow
	case "commands":
		return KindCommand
	default:
		return KindUnknown
	}
}

func ErrorDiagnostic(path string, code string, message string) Diagnostic {
	return Diagnostic{
		Severity: SeverityError,
		Code:     code,
		Path:     path,
		Message:  message,
	}
}

func SortDiagnostics(diagnostics []Diagnostic) {
	sort.Slice(diagnostics, func(i int, j int) bool {
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		if diagnostics[i].Code != diagnostics[j].Code {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
}
