package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func marshalDriverRequest(request any) ([]byte, error) {
	return json.Marshal(request)
}

func invokeDriverCommand(ctx context.Context, envVar string, requestBytes []byte, requestedToolKey string) (Response, error) {
	command := strings.TrimSpace(os.Getenv(envVar))
	if command == "" {
		return Response{}, fmt.Errorf("driver command not configured")
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
		response.ToolKey = requestedToolKey
	}
	if response.ToolKey != requestedToolKey {
		return Response{}, fmt.Errorf("driver response tool_key %q does not match request %q", response.ToolKey, requestedToolKey)
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
