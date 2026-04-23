package web

import (
	"context"
	"strings"

	"odin-os/internal/adapters/browserhuman"
)

const (
	RobinhoodTransferToolKey   = "robinhood_transfer_flow"
	RobinhoodTransferDriverEnv = "ODIN_HUGINN_ROBINHOOD_TRANSFER_DRIVER"
)

type RobinhoodTransferInput struct {
	Mode               string            `json:"mode"`
	Direction          string            `json:"direction"`
	AmountUSD          string            `json:"amount_usd"`
	SourceAccount      string            `json:"source_account"`
	DestinationAccount string            `json:"destination_account"`
	Memo               string            `json:"memo,omitempty"`
	ResumeFacts        map[string]string `json:"resume_facts,omitempty"`
}

type RobinhoodTransferRequest struct {
	ToolKey string                 `json:"tool_key,omitempty"`
	Input   RobinhoodTransferInput `json:"input"`
}

type RobinhoodTransferResponse struct {
	ToolKey   string
	Summary   string
	Artifacts map[string]any
	RawOutput string
}

type RobinhoodTransferDriver struct {
	Driver     browserhuman.Driver
	InvokeFunc func(context.Context, RobinhoodTransferRequest) (RobinhoodTransferResponse, error)
}

func NewRobinhoodTransferDriver() RobinhoodTransferDriver {
	return RobinhoodTransferDriver{
		Driver: browserhuman.Driver{
			EnvVar:         RobinhoodTransferDriverEnv,
			DefaultToolKey: RobinhoodTransferToolKey,
		},
	}
}

func (driver RobinhoodTransferDriver) Invoke(ctx context.Context, request RobinhoodTransferRequest) (RobinhoodTransferResponse, error) {
	if driver.InvokeFunc != nil {
		return driver.InvokeFunc(ctx, request)
	}

	driver = driver.withDefaults()

	response, err := driver.Driver.Invoke(ctx, browserhuman.Request{
		ToolKey:             request.ToolKey,
		AllowDefaultToolKey: true,
		Input:               request.Input,
	})
	if err != nil {
		return RobinhoodTransferResponse{}, err
	}

	return RobinhoodTransferResponse{
		ToolKey:   response.ToolKey,
		Summary:   response.Summary,
		Artifacts: response.Artifacts,
		RawOutput: response.RawOutput,
	}, nil
}

func (driver RobinhoodTransferDriver) withDefaults() RobinhoodTransferDriver {
	defaults := NewRobinhoodTransferDriver()
	if strings.TrimSpace(driver.Driver.EnvVar) == "" {
		driver.Driver.EnvVar = defaults.Driver.EnvVar
	}
	if strings.TrimSpace(driver.Driver.DefaultToolKey) == "" {
		driver.Driver.DefaultToolKey = defaults.Driver.DefaultToolKey
	}
	return driver
}
