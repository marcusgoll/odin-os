package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"odin-os/internal/executors/contract"
	integrationproviders "odin-os/internal/integrations/providers"
)

var (
	ErrNoRouteMatch        = errors.New("no executor route matched task")
	ErrNoExecutorAvailable = errors.New("no executor satisfied route requirements")
)

type Selector struct {
	Config        Config
	ModelRegistry ModelRegistry
	Executors     map[string]contract.Executor
}

type Decision struct {
	RouteName           string
	ExecutorKey         string
	ModelKey            string
	ProviderKey         string
	ProviderModelID     string
	FallbackUsed        bool
	PolicyReason        string
	EstimatedCostUSD    float64
	ContextWindowTokens int
	LatencyTier         string
	Considered          []CandidateDecision
}

type CandidateDecision struct {
	ExecutorKey string
	ModelKey    string
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
		model, modelDecision, ok := selector.modelAllowed(route, executorConfig, spec)
		if !ok {
			decision.Considered = append(decision.Considered, modelDecision)
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
		decision.ModelKey = model.Key
		decision.ProviderKey = model.Provider
		decision.ProviderModelID = model.ProviderModelID
		decision.FallbackUsed = index >= len(route.Preferred)
		decision.PolicyReason = "model_policy_allowed"
		decision.EstimatedCostUSD = model.EstimatedCostUSD(spec.Budget.MaxInputTokens, spec.Budget.MaxOutputTokens)
		decision.ContextWindowTokens = model.ContextWindowTokens
		decision.LatencyTier = model.LatencyTier
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
		if len(route.Match.TaskClasses) > 0 && !hasNormalizedString(route.Match.TaskClasses, normalizedTaskClass(spec)) {
			continue
		}
		if len(route.Match.RiskClasses) > 0 && !hasNormalizedString(route.Match.RiskClasses, normalizedRiskClass(spec)) {
			continue
		}
		return route, true
	}
	return RouteConfig{}, false
}

func (selector Selector) modelAllowed(route RouteConfig, executor ExecutorConfig, spec contract.TaskSpec) (ModelConfig, CandidateDecision, bool) {
	if !selector.ModelRegistry.HasModels() {
		return ModelConfig{}, CandidateDecision{ExecutorKey: executor.Key, Reason: "model_registry_not_configured"}, true
	}
	decision := CandidateDecision{ExecutorKey: executor.Key}
	modelRef := routeModelRef(route, executor)
	if strings.TrimSpace(modelRef) == "" {
		decision.Reason = "missing_model_ref"
		return ModelConfig{}, decision, false
	}
	model, ok := selector.ModelRegistry.ModelByKey(modelRef)
	if !ok {
		decision.ModelKey = modelRef
		decision.Reason = "missing_model_config"
		return ModelConfig{}, decision, false
	}
	decision.ModelKey = model.Key
	if !model.IsEnabled() {
		decision.Reason = "model_disabled"
		return ModelConfig{}, decision, false
	}
	if model.Adapter != executor.Adapter {
		decision.Reason = "model_adapter_mismatch"
		return ModelConfig{}, decision, false
	}
	if blockedByModelTaskClass(model, spec) {
		decision.Reason = "blocked_task_class"
		return ModelConfig{}, decision, false
	}
	if !modelSupportsTaskClass(model, spec) {
		decision.Reason = "unsupported_task_class"
		return ModelConfig{}, decision, false
	}
	if !modelSupportsCapabilities(model, spec.Requirements.CapabilityNeeds) {
		decision.Reason = "model_capability_mismatch"
		return ModelConfig{}, decision, false
	}
	if exceedsModelContext(model, spec) {
		decision.Reason = "context_limit_exceeded"
		return ModelConfig{}, decision, false
	}
	if spec.Budget.MaxCostUSD > 0 && model.EstimatedCostUSD(spec.Budget.MaxInputTokens, spec.Budget.MaxOutputTokens) > spec.Budget.MaxCostUSD {
		decision.Reason = "budget_exceeded"
		return ModelConfig{}, decision, false
	}
	if isHighRiskSpec(spec) && !model.AllowHighRisk && strings.TrimSpace(model.Access) != string(contract.ExecutorClassPlanBackedCLI) {
		decision.Reason = "high_risk_model_blocked"
		return ModelConfig{}, decision, false
	}
	return model, decision, true
}

func routeModelRef(route RouteConfig, executor ExecutorConfig) string {
	if route.ModelRefOverrides != nil {
		if override := strings.TrimSpace(route.ModelRefOverrides[executor.Key]); override != "" {
			return override
		}
	}
	return strings.TrimSpace(executor.ModelRef)
}

func blockedByModelTaskClass(model ModelConfig, spec contract.TaskSpec) bool {
	taskClass := normalizedTaskClass(spec)
	if taskClass == "" {
		return false
	}
	return hasNormalizedString(model.BlockedTaskClasses, taskClass)
}

func modelSupportsTaskClass(model ModelConfig, spec contract.TaskSpec) bool {
	taskClass := normalizedTaskClass(spec)
	if taskClass == "" {
		taskClass = strings.ToLower(strings.TrimSpace(string(spec.Kind)))
	}
	if len(normalizeTokens(model.SupportedTaskClasses)) == 0 {
		return true
	}
	return hasNormalizedString(model.SupportedTaskClasses, taskClass)
}

func modelSupportsCapabilities(model ModelConfig, needs []string) bool {
	for _, need := range normalizeTokens(needs) {
		if !hasNormalizedString(model.Capabilities, need) && !hasNormalizedString(model.SupportedFeatures, need) {
			return false
		}
	}
	return true
}

func exceedsModelContext(model ModelConfig, spec contract.TaskSpec) bool {
	if spec.Budget.MaxInputTokens > 0 && model.MaxInputTokens > 0 && spec.Budget.MaxInputTokens > model.MaxInputTokens {
		return true
	}
	if spec.Budget.MaxOutputTokens > 0 && model.MaxOutputTokens > 0 && spec.Budget.MaxOutputTokens > model.MaxOutputTokens {
		return true
	}
	total := spec.Budget.MaxInputTokens + spec.Budget.MaxOutputTokens
	return total > 0 && model.ContextWindowTokens > 0 && total > model.ContextWindowTokens
}

func isHighRiskSpec(spec contract.TaskSpec) bool {
	riskClass := normalizedRiskClass(spec)
	switch riskClass {
	case "high", "high_risk", "consequential", "external_world", "governance", "destructive":
		return true
	}
	switch normalizedTaskClass(spec) {
	case "finance", "legal", "medical", "security_decision", "approval_resolution", "production_deploy", "public_publish":
		return true
	default:
		return false
	}
}

func normalizedRiskClass(spec contract.TaskSpec) string {
	riskClass := strings.ToLower(strings.TrimSpace(spec.RiskClass))
	if riskClass == "" && spec.Metadata != nil {
		riskClass = strings.ToLower(strings.TrimSpace(spec.Metadata["risk_class"]))
	}
	return riskClass
}

func normalizedTaskClass(spec contract.TaskSpec) string {
	taskClass := strings.ToLower(strings.TrimSpace(spec.TaskClass))
	if taskClass == "" && spec.Metadata != nil {
		taskClass = strings.ToLower(strings.TrimSpace(spec.Metadata["task_class"]))
	}
	return taskClass
}

func hasNormalizedString(values []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, candidate := range values {
		if strings.ToLower(strings.TrimSpace(candidate)) == value {
			return true
		}
	}
	return false
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
