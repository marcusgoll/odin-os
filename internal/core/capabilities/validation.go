package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/core/policy"
	"odin-os/internal/registry"
)

type Error struct {
	CodeValue string
	Message   string
	Cause     error
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Message == "" {
		return err.CodeValue
	}
	return err.CodeValue + ": " + err.Message
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func (err *Error) Code() string {
	if err == nil {
		return ""
	}
	return err.CodeValue
}

var defaultPolicyService = policy.NewService(nil)

func ValidateInvocation(desc Descriptor, req InvokeRequest) error {
	if !desc.Kind.IsInvokable() {
		return nil
	}

	if isEmptySchema(desc.InputSchema) {
		return newValidationError("invokable capability requires inputSchema", nil)
	}
	if isEmptySchema(desc.OutputSchema) {
		return newValidationError("invokable capability requires outputSchema", nil)
	}

	if strings.EqualFold(strings.TrimSpace(desc.InputSchema.Type), "object") {
		if err := validateObjectInput(req.Input); err != nil {
			return newValidationError("capability input must be a JSON object", err)
		}
	}

	return nil
}

func AuthorizeInvocation(ctx context.Context, desc Descriptor, scope ScopeRef, caller CallerRef) error {
	if defaultPolicyService == nil {
		defaultPolicyService = policy.NewService(nil)
	}

	return defaultPolicyService.AuthorizeInvocation(ctx, policy.Descriptor{
		Scope:       strings.TrimSpace(desc.Availability.Scope),
		Scopes:      append([]string(nil), desc.Scopes...),
		Permissions: append([]string(nil), desc.Permissions...),
	}, policy.ScopeRef{
		Kind:       strings.TrimSpace(scope.Kind),
		ProjectKey: strings.TrimSpace(scope.ProjectKey),
	}, policy.CallerRef{
		Kind: strings.TrimSpace(caller.Kind),
		ID:   strings.TrimSpace(caller.ID),
	})
}

func validateObjectInput(input json.RawMessage) error {
	if strings.TrimSpace(string(input)) == "" {
		return errors.New("input is required")
	}

	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return err
	}
	if _, ok := payload.(map[string]any); !ok {
		return fmt.Errorf("expected JSON object input")
	}
	return nil
}

func isEmptySchema(schema registry.SchemaRef) bool {
	return strings.TrimSpace(schema.Ref) == "" && strings.TrimSpace(schema.Type) == ""
}

func newValidationError(message string, cause error) error {
	return &Error{
		CodeValue: "validation_failed",
		Message:   message,
		Cause:     cause,
	}
}
