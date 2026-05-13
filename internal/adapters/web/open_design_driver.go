package web

import (
	"context"
	"strings"

	"odin-os/internal/adapters/browserhuman"
)

const openDesignDriverEnvVar = "ODIN_HUGINN_OPEN_DESIGN_DRIVER"
const openDesignToolKey = "browser_open_design"

type OpenDesignInput struct {
	ArtifactID string `json:"artifact_id"`
	Action     string `json:"action,omitempty"`
	Artifact   any    `json:"artifact,omitempty"`
}

type OpenDesignRequest struct {
	ToolKey string          `json:"tool_key"`
	Input   OpenDesignInput `json:"input"`
}

type OpenDesignResponse struct {
	ToolKey   string         `json:"tool_key"`
	Summary   string         `json:"summary"`
	Artifacts map[string]any `json:"artifacts"`
	RawOutput string         `json:"raw_output"`
}

type OpenDesignDriver struct {
	Driver     browserhuman.Driver
	InvokeFunc func(context.Context, OpenDesignRequest) (OpenDesignResponse, error)
}

func NewOpenDesignDriver() OpenDesignDriver {
	return OpenDesignDriver{
		Driver: browserhuman.Driver{
			EnvVar:         openDesignDriverEnvVar,
			DefaultToolKey: openDesignToolKey,
		},
	}
}

func (driver OpenDesignDriver) Invoke(ctx context.Context, request OpenDesignRequest) (OpenDesignResponse, error) {
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
		return OpenDesignResponse{}, err
	}

	return OpenDesignResponse{
		ToolKey:   response.ToolKey,
		Summary:   response.Summary,
		Artifacts: response.Artifacts,
		RawOutput: response.RawOutput,
	}, nil
}

func (driver OpenDesignDriver) withDefaults() OpenDesignDriver {
	defaults := NewOpenDesignDriver()
	if strings.TrimSpace(driver.Driver.EnvVar) == "" {
		driver.Driver.EnvVar = defaults.Driver.EnvVar
	}
	if strings.TrimSpace(driver.Driver.DefaultToolKey) == "" {
		driver.Driver.DefaultToolKey = defaults.Driver.DefaultToolKey
	}
	return driver
}
