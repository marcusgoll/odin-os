package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMediaEndpointProbeHealthyOnSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := EndpointProbe{}.Check(context.Background(), "plex", server.URL)
	if result.Status != StatusHealthy {
		t.Fatalf("Status = %q, want %q", result.Status, StatusHealthy)
	}
}

func TestMediaEndpointProbeDegradedOnFailureStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	result := EndpointProbe{}.Check(context.Background(), "plex", server.URL)
	if result.Status != StatusDegraded {
		t.Fatalf("Status = %q, want %q", result.Status, StatusDegraded)
	}
}
