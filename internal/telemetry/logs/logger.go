package logs

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"

	redactedValue = "[REDACTED]"
)

var (
	tokenLikePatterns = []*regexp.Regexp{
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`(?:generic-)?token-[A-Za-z0-9_-]{12,}`),
	}
	secretAssignmentPattern = regexp.MustCompile(`(?i)(\b(?:access[_-]?token|refresh[_-]?token|admin-token|token|api[_-]?key|apikey|secret|password|credential|authorization)=)[^&\s,"']+`)
	bearerPattern           = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/-]{12,}`)
)

type Record struct {
	Level         Level          `json:"level"`
	Component     string         `json:"component"`
	Message       string         `json:"message"`
	CorrelationID string         `json:"correlation_id"`
	Scope         string         `json:"scope"`
	ProjectID     *int64         `json:"project_id,omitempty"`
	TaskID        *int64         `json:"task_id,omitempty"`
	RunID         *int64         `json:"run_id,omitempty"`
	Fields        map[string]any `json:"fields,omitempty"`
}

type Logger struct {
	Writer io.Writer
	Now    func() time.Time
}

func (logger Logger) Log(record Record) error {
	now := time.Now().UTC()
	if logger.Now != nil {
		now = logger.Now().UTC()
	}

	payload := struct {
		Timestamp     string         `json:"timestamp"`
		Level         Level          `json:"level"`
		Component     string         `json:"component"`
		Message       string         `json:"message"`
		CorrelationID string         `json:"correlation_id"`
		Scope         string         `json:"scope"`
		ProjectID     *int64         `json:"project_id,omitempty"`
		TaskID        *int64         `json:"task_id,omitempty"`
		RunID         *int64         `json:"run_id,omitempty"`
		Fields        map[string]any `json:"fields,omitempty"`
	}{
		Timestamp:     now.Format(time.RFC3339),
		Level:         record.Level,
		Component:     redactString(record.Component),
		Message:       redactString(record.Message),
		CorrelationID: redactString(record.CorrelationID),
		Scope:         redactString(record.Scope),
		ProjectID:     record.ProjectID,
		TaskID:        record.TaskID,
		RunID:         record.RunID,
		Fields:        redactFields(record.Fields),
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = logger.Writer.Write(encoded)
	return err
}

func redactFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	redacted := make(map[string]any, len(fields))
	for key, value := range fields {
		outputKey := redactString(key)
		if isSensitiveLogKey(key) {
			redacted[outputKey] = redactedValue
			continue
		}
		redacted[outputKey] = redactValue(value)
	}
	return redacted
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case string:
		return redactString(typed)
	case map[string]any:
		return redactFields(typed)
	case map[string]string:
		redacted := make(map[string]any, len(typed))
		for key, value := range typed {
			outputKey := redactString(key)
			if isSensitiveLogKey(key) {
				redacted[outputKey] = redactedValue
				continue
			}
			redacted[outputKey] = redactString(value)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, item := range typed {
			redacted[index] = redactValue(item)
		}
		return redacted
	case []string:
		redacted := make([]string, len(typed))
		for index, item := range typed {
			redacted[index] = redactString(item)
		}
		return redacted
	default:
		return value
	}
}

func redactString(value string) string {
	redacted := value
	for _, pattern := range tokenLikePatterns {
		redacted = pattern.ReplaceAllString(redacted, redactedValue)
	}
	redacted = secretAssignmentPattern.ReplaceAllString(redacted, "${1}"+redactedValue)
	redacted = bearerPattern.ReplaceAllString(redacted, "${1}"+redactedValue)
	return redacted
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, exact := range []string{
		"token",
		"secret",
		"password",
		"credential",
		"authorization",
		"api_key",
		"apikey",
		"client_secret",
		"session_tokens",
	} {
		if normalized == exact {
			return true
		}
	}
	for _, suffix := range []string{
		"_token",
		"_secret",
		"_password",
		"_credential",
		"_authorization",
		"_api_key",
		"_apikey",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}
