package cmd

import (
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

	if !cmd.SilenceErrors {
		t.Fatal("expected SilenceErrors to be set")
	}
	if got != err {
		t.Fatalf("expected original error returned, got %v", got)
	}
}

func TestHandleSessionError_RegularError(t *testing.T) {
	cmd := &cobra.Command{}
	original := fmt.Errorf("connection refused")

	got := handleSessionError(cmd, original)

	if cmd.SilenceErrors {
		t.Fatal("expected SilenceErrors to remain false")
	}
	if !strings.Contains(got.Error(), "failed to create projection session") {
		t.Fatalf("expected wrapped error, got %q", got.Error())
	}
	if !strings.Contains(got.Error(), "connection refused") {
		t.Fatalf("expected original message in wrapped error, got %q", got.Error())
	}
}
