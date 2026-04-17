package health

import (
	"context"
	"path"
	"strings"
	"time"

	shelladapter "odin-os/internal/adapters/shell"
	webadapter "odin-os/internal/adapters/web"
	coremedia "odin-os/internal/core/media"
)

type MediaChecks struct {
	Config       *coremedia.Config
	ProbeCommand string
	ShellProbe   shelladapter.MediaProbe
	WebProbe     webadapter.EndpointProbe
}

func (checks MediaChecks) Checks(ctx context.Context, _ Config, now time.Time) ([]Check, error) {
	if checks.Config == nil || !checks.Config.Enabled {
		return nil, nil
	}

	var results []Check

	if strings.TrimSpace(checks.ProbeCommand) != "" {
		output, err := checks.ShellProbe.Run(ctx, checks.ProbeCommand)
		if err != nil {
			results = append(results, Check{
				Name:       "media.probe",
				Status:     StatusFailed,
				Summary:    "media probe command failed",
				ObservedAt: now,
				Details:    map[string]string{"error": err.Error()},
			})
		} else {
			for _, signal := range output.Signals {
				results = append(results, Check{
					Name:       signal.Name,
					Status:     mediaSignalStatus(signal.Status),
					Summary:    signal.Summary,
					ObservedAt: now,
					Details:    signal.Details,
				})
			}
		}
	}

	for _, service := range checks.Config.Services {
		baseURL := strings.TrimSpace(service.BaseURL)
		if baseURL == "" {
			continue
		}
		endpoint := baseURL
		if healthPath := strings.TrimSpace(service.HealthPath); healthPath != "" {
			endpoint = strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path.Clean(healthPath), "/")
		}

		result := checks.WebProbe.Check(ctx, service.Name, endpoint)
		results = append(results, Check{
			Name:       "media." + service.Name,
			Status:     mediaEndpointStatus(result.Status),
			Summary:    result.Summary,
			ObservedAt: now,
			Details:    result.Details,
		})
	}

	return results, nil
}

func mediaSignalStatus(status string) Status {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "healthy", "ok":
		return StatusHealthy
	case "failed", "critical":
		return StatusFailed
	default:
		return StatusDegraded
	}
}

func mediaEndpointStatus(status webadapter.Status) Status {
	switch status {
	case webadapter.StatusHealthy:
		return StatusHealthy
	default:
		return StatusDegraded
	}
}
