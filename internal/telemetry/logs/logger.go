package logs

import (
	"encoding/json"
	"io"
	"time"
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
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
		Component:     record.Component,
		Message:       record.Message,
		CorrelationID: record.CorrelationID,
		Scope:         record.Scope,
		ProjectID:     record.ProjectID,
		TaskID:        record.TaskID,
		RunID:         record.RunID,
		Fields:        record.Fields,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = logger.Writer.Write(encoded)
	return err
}
