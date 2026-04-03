package engine

import (
	"fmt"
	"testing"
)

type mockProjectionError struct {
	code        string
	description string
}

func (e *mockProjectionError) Error() string            { return e.description }
func (e *mockProjectionError) ErrorCode() string        { return e.code }
func (e *mockProjectionError) ErrorDescription() string { return e.description }

func TestClassifyError_ProjectionError(t *testing.T) {
	err := &mockProjectionError{code: "handler-error", description: "boom"}
	fe := ClassifyError(err)

	if fe.Code != "handler-error" {
		t.Errorf("code: got %q, want %q", fe.Code, "handler-error")
	}
	if fe.Description != "boom" {
		t.Errorf("description: got %q, want %q", fe.Description, "boom")
	}
}

func TestClassifyError_GenericError(t *testing.T) {
	err := fmt.Errorf("something broke")
	fe := ClassifyError(err)

	if fe.Code != "unexpected-error" {
		t.Errorf("code: got %q, want %q", fe.Code, "unexpected-error")
	}
	if fe.Description != "something broke" {
		t.Errorf("description: got %q, want %q", fe.Description, "something broke")
	}
}
