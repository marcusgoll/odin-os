package web

import (
	"context"
	"strings"
)

const defaultDriverEnvVar = "ODIN_HUGINN_DRIVER"
const defaultToolKey = "browser_pbs_session"

type Input struct {
	BidPeriod   string `json:"bid_period"`
	WorkflowKey string `json:"workflow_key"`
	Timezone    string `json:"timezone"`
}

type Request struct {
	ToolKey string `json:"tool_key"`
	Input   Input  `json:"input"`
}

type Response struct {
	Status    string         `json:"status"`
	ToolKey   string         `json:"tool_key"`
	Summary   string         `json:"summary"`
	Artifacts map[string]any `json:"artifacts"`
}

type Driver struct {
	EnvVar string
}

func NewDriver() Driver {
	return Driver{EnvVar: defaultDriverEnvVar}
}

func (driver Driver) Invoke(ctx context.Context, request Request) (Response, error) {
	if request.ToolKey == "" {
		request.ToolKey = defaultToolKey
	}

	requestBytes, err := marshalDriverRequest(request)
	if err != nil {
		return Response{}, err
	}

	response, err := invokeDriverCommand(ctx, driver.envVar(), requestBytes, request.ToolKey)
	if err != nil {
		return Response{}, err
	}
	if response.ToolKey == "" {
		response.ToolKey = request.ToolKey
	}
	if response.Artifacts == nil {
		response.Artifacts = map[string]any{}
	}

	return response, nil
}

func (driver Driver) envVar() string {
	if strings.TrimSpace(driver.EnvVar) == "" {
		return defaultDriverEnvVar
	}
	return driver.EnvVar
}
