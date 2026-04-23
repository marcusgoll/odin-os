package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const defaultDriverEnvVar = "ODIN_GOOGLE_CALENDAR_DRIVER"
const defaultToolKey = "google_calendar_off_dates"

type Input struct {
	BidPeriod  string `json:"bid_period"`
	CalendarID string `json:"calendar_id"`
	Timezone   string `json:"timezone"`
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
	command := strings.TrimSpace(os.Getenv(driver.envVar()))
	if command == "" {
		return Response{}, fmt.Errorf("driver command not configured")
	}

	if request.ToolKey == "" {
		request.ToolKey = defaultToolKey
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}

	commandParts := strings.Fields(command)
	if len(commandParts) == 0 {
		return Response{}, fmt.Errorf("driver command not configured")
	}

	cmd := exec.CommandContext(ctx, commandParts[0], commandParts[1:]...)
	cmd.Stdin = bytes.NewReader(requestBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("driver command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var response Response
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return Response{}, fmt.Errorf("decode driver response: %w", err)
	}
	if response.ToolKey == "" {
		response.ToolKey = request.ToolKey
	}
	if response.ToolKey != request.ToolKey {
		return Response{}, fmt.Errorf("driver response tool_key %q does not match request %q", response.ToolKey, request.ToolKey)
	}
	if response.Status == "" {
		return Response{}, fmt.Errorf("driver response status is empty")
	}
	if !strings.EqualFold(strings.TrimSpace(response.Status), "completed") {
		return Response{}, fmt.Errorf("driver response status %q is not completed", response.Status)
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
