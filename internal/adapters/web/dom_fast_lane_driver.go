package web

import (
	"context"
	"strings"
)

const domFastLaneDriverEnvVar = "ODIN_HUGINN_DOM_FAST_LANE_DRIVER"
const domFastLaneToolKey = "browser_dom_fast_lane"

type DOMFastLaneInput struct {
	RecipeKey     string `json:"recipe_key"`
	TargetURL     string `json:"target_url"`
	Label         string `json:"label,omitempty"`
	WaitMS        string `json:"wait_ms,omitempty"`
	Headless      string `json:"headless,omitempty"`
	AllowedDomain string `json:"allowed_domain,omitempty"`
}

type DOMFastLaneRequest struct {
	ToolKey string           `json:"tool_key"`
	Input   DOMFastLaneInput `json:"input"`
}

type DOMFastLaneDriver struct {
	EnvVar string
}

func NewDOMFastLaneDriver() DOMFastLaneDriver {
	return DOMFastLaneDriver{EnvVar: domFastLaneDriverEnvVar}
}

func (driver DOMFastLaneDriver) Invoke(ctx context.Context, request DOMFastLaneRequest) (Response, error) {
	if request.ToolKey == "" {
		request.ToolKey = domFastLaneToolKey
	}

	requestBytes, err := marshalDriverRequest(request)
	if err != nil {
		return Response{}, err
	}

	return invokeDriverCommandAllowStatuses(ctx, driver.envVar(), requestBytes, request.ToolKey, map[string]struct{}{
		"completed": {},
		"blocked":   {},
	})
}

func (driver DOMFastLaneDriver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return domFastLaneDriverEnvVar
	}
	return driver.EnvVar
}
