package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type mockProjectionError struct {
	code string
	desc string
	msg  string
}

func (e *mockProjectionError) Error() string            { return e.msg }
func (e *mockProjectionError) ErrorCode() string        { return e.code }
func (e *mockProjectionError) ErrorDescription() string { return e.desc }

func TestHandleSessionError_ProjectionError(t *testing.T) {
	cmd := &cobra.Command{}
	err := &mockProjectionError{
		code: "test-error",
		desc: "something went wrong",
		msg:  "test error message",
	}

	got := handleSessionError(cmd, err)

	var silent *silentError
	if !errors.As(got, &silent) {
		t.Fatalf("expected silentError, got %T", got)
	}
	if silent.err != err {
		t.Fatalf("expected wrapped original error, got %v", silent.err)
	}
}

func TestHandleSessionError_RegularError(t *testing.T) {
	cmd := &cobra.Command{}
	original := fmt.Errorf("connection refused")

	got := handleSessionError(cmd, original)

	var silent *silentError
	if errors.As(got, &silent) {
		t.Fatal("expected non-silent error for generic case")
	}
	if !strings.Contains(got.Error(), "failed to create projection session") {
		t.Fatalf("expected wrapped error, got %q", got.Error())
	}
	if !strings.Contains(got.Error(), "connection refused") {
		t.Fatalf("expected original message in wrapped error, got %q", got.Error())
	}
}
