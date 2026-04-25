package commands

import (
	"fmt"
	"strings"
)

type ApprovalSupportFilter string

const (
	ApprovalSupportAll         ApprovalSupportFilter = "all"
	ApprovalSupportSupported   ApprovalSupportFilter = "supported"
	ApprovalSupportUnsupported ApprovalSupportFilter = "unsupported"
)

const ApprovalListUsage = "usage: approvals [all|supported|unsupported]"

func ParseApprovalSupportFilter(args []string) (ApprovalSupportFilter, error) {
	if len(args) == 0 {
		return ApprovalSupportAll, nil
	}
	if len(args) != 1 {
		return "", fmt.Errorf(ApprovalListUsage)
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "", "all":
		return ApprovalSupportAll, nil
	case string(ApprovalSupportSupported):
		return ApprovalSupportSupported, nil
	case string(ApprovalSupportUnsupported):
		return ApprovalSupportUnsupported, nil
	default:
		return "", fmt.Errorf(ApprovalListUsage)
	}
}

func (filter ApprovalSupportFilter) Matches(resolverSupport string) bool {
	switch filter {
	case ApprovalSupportAll:
		return true
	case ApprovalSupportSupported:
		return resolverSupport == string(ApprovalSupportSupported)
	case ApprovalSupportUnsupported:
		return resolverSupport == string(ApprovalSupportUnsupported)
	default:
		return false
	}
}
