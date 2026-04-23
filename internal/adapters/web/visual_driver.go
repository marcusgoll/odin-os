package web

import (
	"context"
	"strings"
)

const visualDriverEnvVar = "ODIN_HUGINN_VISUAL_DRIVER"
const visualToolKey = "browser_visual_audit"

type VisualInput struct {
	TargetURL        string `json:"target_url"`
	Label            string `json:"label"`
	ScreenshotPath   string `json:"screenshot_path,omitempty"`
	WaitMS           string `json:"wait_ms,omitempty"`
	AllowPrivateHost string `json:"allow_private_host,omitempty"`
	Headless         string `json:"headless,omitempty"`
}

type VisualRequest struct {
	ToolKey string      `json:"tool_key"`
	Input   VisualInput `json:"input"`
}

type VisualDriver struct {
	EnvVar string
}

func NewVisualDriver() VisualDriver {
	return VisualDriver{EnvVar: visualDriverEnvVar}
}

func (driver VisualDriver) Invoke(ctx context.Context, request VisualRequest) (Response, error) {
	if request.ToolKey == "" {
		request.ToolKey = visualToolKey
	}

	requestBytes, err := marshalDriverRequest(request)
	if err != nil {
		return Response{}, err
	}

	return invokeDriverCommand(ctx, driver.envVar(), requestBytes, request.ToolKey)
}

func (driver VisualDriver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return visualDriverEnvVar
	}
	return driver.EnvVar
}
