package capabilities

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

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

	if err := validateDeclaredInputType(req.Input, desc.InputSchema.Type); err != nil {
		return newValidationError("capability input must match inputSchema.type", err)
	}

	return nil
}

func isEmptySchema(schema registry.SchemaRef) bool {
	return strings.TrimSpace(schema.Ref) == "" && strings.TrimSpace(schema.Type) == ""
}

func validateDeclaredInputType(input json.RawMessage, declaredType string) error {
	declaredType = strings.ToLower(strings.TrimSpace(declaredType))
	if declaredType == "" {
		return nil
	}

	if strings.TrimSpace(string(input)) == "" {
		return errors.New("input is required")
	}

	var payload any
	if err := json.Unmarshal(input, &payload); err != nil {
		return err
	}

	switch declaredType {
	case "object":
		if _, ok := payload.(map[string]any); !ok {
			return fmt.Errorf("expected JSON object input")
		}
	case "array":
		if _, ok := payload.([]any); !ok {
			return fmt.Errorf("expected JSON array input")
		}
	case "string":
		if _, ok := payload.(string); !ok {
			return fmt.Errorf("expected JSON string input")
		}
	case "number":
		if _, ok := payload.(float64); !ok {
			return fmt.Errorf("expected JSON number input")
		}
	case "integer":
		num, ok := payload.(float64)
		if !ok || math.Trunc(num) != num {
			return fmt.Errorf("expected JSON integer input")
		}
	case "boolean":
		if _, ok := payload.(bool); !ok {
			return fmt.Errorf("expected JSON boolean input")
		}
	case "null":
		if payload != nil {
			return fmt.Errorf("expected JSON null input")
		}
	default:
		return nil
	}

	return nil
}

func newValidationError(message string, cause error) error {
	return &Error{
		CodeValue: "validation_failed",
		Message:   message,
		Cause:     cause,
	}
}
