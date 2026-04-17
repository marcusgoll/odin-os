package tools

import (
	"fmt"

	"odin-os/internal/tools/catalog"
)

type Service struct {
	Definitions   map[string]catalog.ToolDefinition
	AuthorizeFunc func(ToolRequest) AuthorizationResult
}

func (service Service) Authorize(request ToolRequest) AuthorizationResult {
	if service.AuthorizeFunc == nil {
		return AuthorizationResult{Allowed: true}
	}
	return service.AuthorizeFunc(request)
}

func (service Service) Invoke(request ToolRequest) (ToolResult, error) {
	authorization := service.Authorize(request)
	if !authorization.Allowed {
		return ToolResult{}, fmt.Errorf("tool %q denied: %s", request.ToolKey, authorization.Reason)
	}

	definition, ok := service.Definitions[request.ToolKey]
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown tool %q", request.ToolKey)
	}
	if definition.Invoke == nil {
		return ToolResult{}, fmt.Errorf("tool %q is not invokable", request.ToolKey)
	}

	parameters := request.Parameters
	if parameters == nil {
		parameters = map[string]string{}
	}
	result, err := definition.Invoke(parameters)
	if err != nil {
		return ToolResult{}, err
	}

	artifacts := make([]ArtifactReference, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		artifacts = append(artifacts, ArtifactReference{
			Kind: "raw_ref",
			Ref:  artifact,
		})
	}

	return ToolResult{
		ToolKey:         request.ToolKey,
		Summary:         result.Summary,
		Artifacts:       artifacts,
		KeyFacts:        cloneStringMap(result.KeyFacts),
		FollowOnOptions: append([]string(nil), result.FollowOnOptions...),
		RawRef:          result.RawRef,
		RawOutput:       result.RawOutput,
	}, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
