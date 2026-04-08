package registry

import (
	"sort"
	"strings"
)

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

type Frontmatter struct {
	Kind       Kind     `yaml:"kind"`
	Key        string   `yaml:"key"`
	Title      string   `yaml:"title"`
	Summary    string   `yaml:"summary"`
	Status     string   `yaml:"status"`
	Tags       []string `yaml:"tags"`
	Owners     []string `yaml:"owners"`
	Role       string   `yaml:"role"`
	Scopes     []string `yaml:"scopes"`
	Tools      []string `yaml:"tools"`
	Strictness string   `yaml:"strictness"`
	AppliesTo  []string `yaml:"applies_to"`
	Entrypoint string   `yaml:"entrypoint"`
	Composes   []string `yaml:"composes"`
	Command    string   `yaml:"command"`
	Aliases    []string `yaml:"aliases"`
}

type ParsedDocument struct {
	Source       SourceFile
	Frontmatter  Frontmatter
	Body         string
	Sections     map[string]string
	SectionOrder []string
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
	Kind       Kind
	Key        string
	Title      string
	Summary    string
	Status     string
	Tags       []string
	Owners     []string
	Role       string
	Scopes     []string
	Tools      []string
	Strictness string
	AppliesTo  []string
	Entrypoint string
	Composes   []string
	Command    string
	Aliases    []string
	Sections   map[string]string
	Source     SourceInfo
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
