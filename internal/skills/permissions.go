package skills

import "odin-os/internal/skills/permissionspec"

type PermissionKind = permissionspec.Kind

const (
	PermissionKindRepoRead              PermissionKind = permissionspec.KindRepoRead
	PermissionKindRuntimeRead           PermissionKind = permissionspec.KindRuntimeRead
	PermissionKindRepoMutateIsolated    PermissionKind = permissionspec.KindRepoMutateIsolated
	PermissionKindRepoMutateFull        PermissionKind = permissionspec.KindRepoMutateFull
	PermissionKindRepoMutateGovernance  PermissionKind = permissionspec.KindRepoMutateGovernance
	PermissionKindRepoMutateDestructive PermissionKind = permissionspec.KindRepoMutateDestructive
)

type Permission = permissionspec.Permission

func ParsePermission(raw string) (Permission, error) {
	return permissionspec.Parse(raw)
}
