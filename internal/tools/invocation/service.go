package invocation

import (
	"context"
	"encoding/json"

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
	driver := browserhuman.NewDriver()
	if service.Driver.EnvVar != "" {
		driver.EnvVar = service.Driver.EnvVar
	}
	if service.Driver.DefaultToolKey != "" {
		driver.DefaultToolKey = service.Driver.DefaultToolKey
	}

	response, err := driver.Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response)
}

func toResult(toolKey string, summary string, artifacts map[string]any, response any) (Result, error) {
	rawOutput, err := json.Marshal(response)
	if err != nil {
		return Result{}, err
	}

	return Result{
		ToolKey:   toolKey,
		Summary:   summary,
		Artifacts: cloneArtifacts(artifacts),
		RawOutput: string(rawOutput),
	}, nil
}

func cloneArtifacts(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
