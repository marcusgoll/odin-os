package permissionspec

import (
	"fmt"
	"regexp"
	"strings"
)

type Kind string

const (
	KindRepoRead              Kind = "repo.read"
	KindRuntimeRead           Kind = "runtime.read"
	KindRepoMutateIsolated    Kind = "repo.mutate.isolated"
	KindRepoMutateFull        Kind = "repo.mutate.full"
	KindRepoMutateGovernance  Kind = "repo.mutate.governance"
	KindRepoMutateDestructive Kind = "repo.mutate.destructive"
)

const isolatedPermissionPrefix = "repo.mutate.isolated:"

var actionKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)

type Permission struct {
	Raw       string
	Kind      Kind
	ActionKey string
}

func Parse(raw string) (Permission, error) {
	if raw == "" {
		return Permission{}, fmt.Errorf("permission is required")
	}
	if strings.TrimSpace(raw) != raw {
		return Permission{}, fmt.Errorf("permission %q must not include surrounding whitespace", raw)
	}

	switch raw {
	case string(KindRepoRead):
		return Permission{Raw: raw, Kind: KindRepoRead}, nil
	case string(KindRuntimeRead):
		return Permission{Raw: raw, Kind: KindRuntimeRead}, nil
	case string(KindRepoMutateFull):
		return Permission{Raw: raw, Kind: KindRepoMutateFull}, nil
	case string(KindRepoMutateGovernance):
		return Permission{Raw: raw, Kind: KindRepoMutateGovernance}, nil
	case string(KindRepoMutateDestructive):
		return Permission{Raw: raw, Kind: KindRepoMutateDestructive}, nil
	}

	if !strings.HasPrefix(raw, isolatedPermissionPrefix) {
		return Permission{}, fmt.Errorf("unknown permission %q", raw)
	}

	actionKey := strings.TrimPrefix(raw, isolatedPermissionPrefix)
	if actionKey == "" {
		return Permission{}, fmt.Errorf("isolated permission %q requires an action key", raw)
	}
	if !actionKeyPattern.MatchString(actionKey) {
		return Permission{}, fmt.Errorf("isolated permission %q has an invalid action key", raw)
	}

	return Permission{
		Raw:       raw,
		Kind:      KindRepoMutateIsolated,
		ActionKey: actionKey,
	}, nil
}
