package invocation

import (
	"context"
	"encoding/json"

	caldriver "odin-os/internal/adapters/calendar"
	webdriver "odin-os/internal/adapters/web"
)

type Result struct {
	ToolKey   string
	Summary   string
	Artifacts map[string]any
	RawOutput string
}

type Service struct{}

func (service Service) GoogleCalendarOffDates(ctx context.Context, request caldriver.Request) (Result, error) {
	response, err := caldriver.NewDriver().Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnPBSSession(ctx context.Context, request webdriver.Request) (Result, error) {
	response, err := webdriver.NewDriver().Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnVisualAudit(ctx context.Context, request webdriver.VisualRequest) (Result, error) {
	response, err := webdriver.NewVisualDriver().Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnXPostVisibleEvidence(ctx context.Context, request webdriver.XPostRequest) (Result, error) {
	response, err := webdriver.NewXPostDriver().Invoke(ctx, request)
	if err != nil {
		return Result{}, err
	}
	return toResult(response.ToolKey, response.Summary, response.Artifacts, response)
}

func (service Service) HuginnXPostPublish(ctx context.Context, request webdriver.XPublishRequest) (Result, error) {
	response, err := webdriver.NewXPublishDriver().Invoke(ctx, request)
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
