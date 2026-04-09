package router

import (
	"context"
	"errors"
	"fmt"

	"odin-os/internal/executors/contract"
)

var (
	ErrNoRouteMatch        = errors.New("no executor route matched task")
	ErrNoExecutorAvailable = errors.New("no executor satisfied route requirements")
)

type Selector struct {
	Config    Config
	Executors map[string]contract.Executor
}

type Decision struct {
	RouteName    string
	ExecutorKey  string
	FallbackUsed bool
	Considered   []CandidateDecision
}

type CandidateDecision struct {
	ExecutorKey string
	Reason      string
}

func (selector Selector) Select(ctx context.Context, spec contract.TaskSpec) (Decision, error) {
	route, ok := selector.matchRoute(spec)
	if !ok {
		return Decision{}, ErrNoRouteMatch
	}

	order := append([]string{}, route.Preferred...)
	order = append(order, route.Fallback...)

	decision := Decision{
		RouteName:  route.Name,
		Considered: make([]CandidateDecision, 0, len(order)),
	}

	for index, key := range order {
		executorConfig, ok := selector.Config.ExecutorByKey(key)
		if !ok {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "not_configured"})
			continue
		}
		if !executorConfig.Enabled {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "disabled"})
			continue
		}

		executor, ok := selector.Executors[key]
		if !ok {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "not_registered"})
			continue
		}
		if executor.Class() != executorConfig.Class {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "class_mismatch"})
			continue
		}

		health, err := executor.Health(ctx)
		if err != nil {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "health_error"})
			continue
		}
		if health.Status == contract.HealthStatusUnavailable {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "unavailable"})
			continue
		}

		capabilities, err := executor.Capabilities(ctx)
		if err != nil {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "capabilities_error"})
			continue
		}
		if !capabilities.Matches(spec) {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "capability_mismatch"})
			continue
		}

		decision.ExecutorKey = key
		decision.FallbackUsed = index >= len(route.Preferred)
		return decision, nil
	}

	return decision, fmt.Errorf("%w for route %q", ErrNoExecutorAvailable, route.Name)
}

func (selector Selector) matchRoute(spec contract.TaskSpec) (RouteConfig, bool) {
	for _, route := range selector.Config.Routes {
		if len(route.Match.TaskKinds) > 0 && !hasTaskKind(route.Match.TaskKinds, spec.Kind) {
			continue
		}
		if len(route.Match.Scopes) > 0 && !hasString(route.Match.Scopes, spec.Scope) {
			continue
		}
		return route, true
	}
	return RouteConfig{}, false
}

func hasTaskKind(values []contract.TaskKind, value contract.TaskKind) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func hasString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
