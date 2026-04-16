package invocation

import (
	"context"

	"odin-os/internal/adapters/browserhuman"
)

type Result struct {
	ToolKey   string
	Summary   string
	Artifacts map[string]any
	RawOutput string
}

type Service struct {
	Driver browserhuman.Driver
}

func (service Service) BrowserHuman(ctx context.Context, request browserhuman.Request) (Result, error) {
	driver := service.Driver.WithDefaults()

	response, err := driver.Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response.RawOutput)
}

func toResult(toolKey string, summary string, artifacts map[string]any, rawOutput string) (Result, error) {
	return Result{
		ToolKey:   toolKey,
		Summary:   summary,
		Artifacts: cloneArtifacts(artifacts),
		RawOutput: rawOutput,
	}, nil
}

func cloneArtifacts(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = cloneArtifactValue(value)
	}
	return cloned
}

func cloneArtifactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArtifacts(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneArtifactValue(item)
		}
		return cloned
	default:
		return typed
	}
}
