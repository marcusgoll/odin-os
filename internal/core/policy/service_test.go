package policy

import (
	"context"
	"errors"
	"testing"
)

type codedError interface {
	Code() string
}

func errorCode(err error) (string, bool) {
	var coded codedError
	if errors.As(err, &coded) {
		return coded.Code(), true
	}
	return "", false
}

func TestServiceRejectsDeniedPermission(t *testing.T) {
	service := NewService(nil)

	err := service.AuthorizeInvocation(context.Background(), Descriptor{
		Scope:       "project",
		Permissions: []string{"filesystem"},
	}, ScopeRef{Kind: "project"}, CallerRef{Kind: "guest"})
	if err == nil {
		t.Fatal("AuthorizeInvocation() error = nil, want permission denied")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("AuthorizeInvocation() error = %v, want coded policy error", err)
	}
	if code != "permission_denied" {
		t.Fatalf("AuthorizeInvocation() code = %q, want %q", code, "permission_denied")
	}
}

func TestServiceRejectsInvalidScope(t *testing.T) {
	service := NewService(nil)

	err := service.AuthorizeInvocation(context.Background(), Descriptor{
		Scope: "project",
	}, ScopeRef{Kind: "global"}, CallerRef{Kind: "cli"})
	if err == nil {
		t.Fatal("AuthorizeInvocation() error = nil, want invalid scope")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("AuthorizeInvocation() error = %v, want coded policy error", err)
	}
	if code != "invalid_scope" {
		t.Fatalf("AuthorizeInvocation() code = %q, want %q", code, "invalid_scope")
	}
}

func TestServiceRejectsEmptyCallerIdentity(t *testing.T) {
	service := NewService(nil)

	err := service.AuthorizeInvocation(context.Background(), Descriptor{
		Scope: "project",
	}, ScopeRef{Kind: "project"}, CallerRef{})
	if err == nil {
		t.Fatal("AuthorizeInvocation() error = nil, want permission denied")
	}

	code, ok := errorCode(err)
	if !ok {
		t.Fatalf("AuthorizeInvocation() error = %v, want coded policy error", err)
	}
	if code != "permission_denied" {
		t.Fatalf("AuthorizeInvocation() code = %q, want %q", code, "permission_denied")
	}
}

func TestServiceDecidesApprovalRequirement(t *testing.T) {
	service := NewService(nil)

	tests := []struct {
		name    string
		request ApprovalRequest
		want    ApprovalDecision
	}{
		{
			name: "allowed read only",
			request: ApprovalRequest{
				Subject: "tool project_status",
			},
			want: ApprovalDecision{
				Allowed: true,
				Code:    "allowed",
			},
		},
		{
			name: "approval required",
			request: ApprovalRequest{
				Subject:  "tool browser_x_post_publish",
				Required: true,
				Reason:   "public social publishing requires an approved social_outcome",
			},
			want: ApprovalDecision{
				ApprovalRequired: true,
				Code:             "approval_required",
				Reason:           "public social publishing requires an approved social_outcome",
			},
		},
		{
			name: "approved execution",
			request: ApprovalRequest{
				Subject:  "task 42",
				Required: true,
				Status:   ApprovalStatusApproved,
			},
			want: ApprovalDecision{
				Allowed: true,
				Code:    "allowed",
			},
		},
		{
			name: "denied execution",
			request: ApprovalRequest{
				Subject:  "task 42",
				Required: true,
				Status:   ApprovalStatusDenied,
				Reason:   "operator denied",
			},
			want: ApprovalDecision{
				Denied: true,
				Code:   "approval_denied",
				Reason: "operator denied",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			decision := service.DecideApproval(context.Background(), tt.request)
			if decision.Allowed != tt.want.Allowed || decision.ApprovalRequired != tt.want.ApprovalRequired || decision.Denied != tt.want.Denied || decision.Code != tt.want.Code || decision.Reason != tt.want.Reason {
				t.Fatalf("DecideApproval() = %+v, want %+v", decision, tt.want)
			}
		})
	}
}
