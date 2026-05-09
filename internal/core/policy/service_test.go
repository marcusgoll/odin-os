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
