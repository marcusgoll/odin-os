package policy

import (
	"context"
	"fmt"
	"strings"
)

type Descriptor struct {
	Scope       string
	Scopes      []string
	Permissions []string
}

type ScopeRef struct {
	Kind       string
	ProjectKey string
}

type CallerRef struct {
	Kind string
	ID   string
}

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

type Service struct {
	callerPermissions map[string]map[string]struct{}
}

func NewService(allowlist map[string][]string) *Service {
	service := &Service{
		callerPermissions: defaultCallerPermissions(),
	}
	if len(allowlist) > 0 {
		service.callerPermissions = normalizeAllowlist(allowlist)
	}
	return service
}

func (service *Service) AuthorizeInvocation(_ context.Context, desc Descriptor, scope ScopeRef, caller CallerRef) error {
	if service == nil {
		return nil
	}

	if scope.Kind != "" && !matchesScope(desc, scope.Kind) {
		return &Error{
			CodeValue: "invalid_scope",
			Message:   fmt.Sprintf("scope %q is not allowed for capability scoped to %q", scope.Kind, strings.TrimSpace(desc.Scope)),
		}
	}

	if caller.Kind == "" || len(desc.Permissions) == 0 {
		return nil
	}

	allowedPermissions, ok := service.callerPermissions[caller.Kind]
	if !ok {
		return &Error{
			CodeValue: "permission_denied",
			Message:   fmt.Sprintf("caller kind %q is not allowlisted for capability permissions", caller.Kind),
		}
	}
	for _, required := range desc.Permissions {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if _, ok := allowedPermissions[required]; !ok {
			return &Error{
				CodeValue: "permission_denied",
				Message:   fmt.Sprintf("caller kind %q is not allowlisted for permission %q", caller.Kind, required),
			}
		}
	}

	return nil
}

func matchesScope(desc Descriptor, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}

	allowedScopes := append([]string{strings.TrimSpace(desc.Scope)}, desc.Scopes...)
	for _, candidate := range allowedScopes {
		if strings.TrimSpace(candidate) == requested {
			return true
		}
	}
	return false
}

func normalizeAllowlist(allowlist map[string][]string) map[string]map[string]struct{} {
	normalized := make(map[string]map[string]struct{}, len(allowlist))
	for callerKind, permissions := range allowlist {
		callerKind = strings.TrimSpace(callerKind)
		if callerKind == "" {
			continue
		}
		if _, ok := normalized[callerKind]; !ok {
			normalized[callerKind] = make(map[string]struct{}, len(permissions))
		}
		for _, permission := range permissions {
			permission = strings.TrimSpace(permission)
			if permission == "" {
				continue
			}
			normalized[callerKind][permission] = struct{}{}
		}
	}
	return normalized
}

func defaultCallerPermissions() map[string]map[string]struct{} {
	return normalizeAllowlist(map[string][]string{
		"api": {
			"filesystem",
			"web",
		},
		"cli": {
			"filesystem",
			"web",
		},
		"shell": {
			"filesystem",
			"web",
		},
		"system": {
			"filesystem",
			"web",
		},
		"workflow": {
			"filesystem",
			"web",
		},
	})
}
