package web

import (
	"context"
	"strings"
)

const xPublishDriverEnvVar = "ODIN_HUGINN_X_PUBLISH_DRIVER"
const xPublishToolKey = "browser_x_post_publish"

type XPublishInput struct {
	PostText       string `json:"post_text"`
	ContentKind    string `json:"content_kind,omitempty"`
	InReplyToURL   string `json:"in_reply_to_url,omitempty"`
	Label          string `json:"label"`
	ScreenshotPath string `json:"screenshot_path,omitempty"`
	WaitMS         string `json:"wait_ms,omitempty"`
	Headless       string `json:"headless,omitempty"`
}

type XPublishRequest struct {
	ToolKey string        `json:"tool_key"`
	Input   XPublishInput `json:"input"`
}

type XPublishDriver struct {
	EnvVar string
}

func NewXPublishDriver() XPublishDriver {
	return XPublishDriver{EnvVar: xPublishDriverEnvVar}
}

func (driver XPublishDriver) Invoke(ctx context.Context, request XPublishRequest) (Response, error) {
	if request.ToolKey == "" {
		request.ToolKey = xPublishToolKey
	}

	requestBytes, err := marshalDriverRequest(request)
	if err != nil {
		return Response{}, err
	}

	return invokeDriverCommand(ctx, driver.envVar(), requestBytes, request.ToolKey)
}

func (driver XPublishDriver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return xPublishDriverEnvVar
	}
	return driver.EnvVar
}
