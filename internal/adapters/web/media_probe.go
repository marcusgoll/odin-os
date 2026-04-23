package web

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type Status string

const (
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
)

type EndpointResult struct {
	Name    string
	Status  Status
	Summary string
	Details map[string]string
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type EndpointProbe struct {
	Client HTTPDoer
}

func (probe EndpointProbe) Check(ctx context.Context, name string, endpoint string) EndpointResult {
	client := probe.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return EndpointResult{
			Name:    name,
			Status:  StatusDegraded,
			Summary: "service endpoint is invalid",
			Details: map[string]string{"error": err.Error(), "endpoint": endpoint},
		}
	}

	response, err := client.Do(request)
	if err != nil {
		return EndpointResult{
			Name:    name,
			Status:  StatusDegraded,
			Summary: "service endpoint is unreachable",
			Details: map[string]string{"error": err.Error(), "endpoint": endpoint},
		}
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return EndpointResult{
			Name:   name,
			Status: StatusDegraded,
			Summary: fmt.Sprintf(
				"service endpoint returned %d",
				response.StatusCode,
			),
			Details: map[string]string{
				"endpoint":    endpoint,
				"status_code": fmt.Sprintf("%d", response.StatusCode),
			},
		}
	}

	return EndpointResult{
		Name:    name,
		Status:  StatusHealthy,
		Summary: "service endpoint is reachable",
		Details: map[string]string{"endpoint": endpoint},
	}
}
