package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"odin-os/internal/executors/contract"
	integrationproviders "odin-os/internal/integrations/providers"
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

	providerService := integrationproviders.Service{Executors: selector.Executors}

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

		profile, err := providerService.CapabilityProfile(ctx, key)
		if err != nil {
			var providerErr integrationproviders.ProviderError
			if errors.As(err, &providerErr) && providerErr.Code == "provider_not_registered" {
				decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "not_registered"})
				continue
			}
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "capabilities_error"})
			continue
		}
		if profile.ExecutorClass != executorConfig.Class {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "class_mismatch"})
			continue
		}

		health, err := providerService.Health(ctx, key)
		if err != nil {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "health_error"})
			continue
		}
		if health.Status == contract.HealthStatusUnavailable {
			decision.Considered = append(decision.Considered, CandidateDecision{ExecutorKey: key, Reason: "unavailable"})
			continue
		}

		if !profile.Matches(spec) {
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

type RoutingRefinement struct {
	RouteName  string
	Preferred  []string
	Fallback   []string
	SourceKind string
	SourceID   int64
}

type routingRefinementPayload struct {
	Preferred []string `json:"preferred"`
	Fallback  []string `json:"fallback"`
	Executor  string   `json:"executor"`
}

func ApplyRoutingRefinements(cfg Config, refinements []RoutingRefinement) (Config, error) {
	if len(refinements) == 0 {
		return cfg, nil
	}

	applied := Config{
		Version:   cfg.Version,
		Executors: append([]ExecutorConfig{}, cfg.Executors...),
		Routes:    make([]RouteConfig, len(cfg.Routes)),
	}
	copy(applied.Routes, cfg.Routes)

	for _, refinement := range refinements {
		index := -1
		for routeIndex, route := range applied.Routes {
			if route.Name == refinement.RouteName {
				index = routeIndex
				break
			}
		}
		if index == -1 {
			return Config{}, fmt.Errorf("route refinement references unknown route %q", refinement.RouteName)
		}

		if len(refinement.Preferred) > 0 {
			applied.Routes[index].Preferred = append([]string{}, refinement.Preferred...)
		}
		if len(refinement.Fallback) > 0 {
			applied.Routes[index].Fallback = append([]string{}, refinement.Fallback...)
		}
	}

	return applied, nil
}

func ParseRoutingRefinementChange(changePayloadJSON string, routeName string, sourceID int64) (RoutingRefinement, error) {
	var payload routingRefinementPayload
	if err := json.Unmarshal([]byte(changePayloadJSON), &payload); err != nil {
		return RoutingRefinement{}, err
	}

	refinement := RoutingRefinement{
		RouteName:  routeName,
		Preferred:  append([]string{}, payload.Preferred...),
		Fallback:   append([]string{}, payload.Fallback...),
		SourceKind: "promotion",
		SourceID:   sourceID,
	}
	if payload.Executor != "" && len(refinement.Preferred) == 0 {
		refinement.Preferred = []string{payload.Executor}
	}
	return refinement, nil
}
