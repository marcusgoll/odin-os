package web

import (
	"context"
	"strings"
)

const xPostDriverEnvVar = "ODIN_HUGINN_X_POST_DRIVER"
const xPostToolKey = "browser_x_post_visible_evidence"

type XPostInput struct {
	TargetURL      string `json:"target_url"`
	Label          string `json:"label"`
	ScreenshotPath string `json:"screenshot_path,omitempty"`
	WaitMS         string `json:"wait_ms,omitempty"`
	Headless       string `json:"headless,omitempty"`
}

type XPostRequest struct {
	ToolKey string     `json:"tool_key"`
	Input   XPostInput `json:"input"`
}

type XPostDriver struct {
	EnvVar string
}

func NewXPostDriver() XPostDriver {
	return XPostDriver{EnvVar: xPostDriverEnvVar}
}

func (driver XPostDriver) Invoke(ctx context.Context, request XPostRequest) (Response, error) {
	if request.ToolKey == "" {
		request.ToolKey = xPostToolKey
	}

	requestBytes, err := marshalDriverRequest(request)
	if err != nil {
		return Response{}, err
	}

	return invokeDriverCommand(ctx, driver.envVar(), requestBytes, request.ToolKey)
}

func (driver XPostDriver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return xPostDriverEnvVar
	}
	return driver.EnvVar
}
