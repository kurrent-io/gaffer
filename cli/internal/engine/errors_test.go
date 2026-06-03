package engine

import (
	"fmt"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

type mockProjectionError struct {
	code        string
	description string
}

func (e *mockProjectionError) Error() string            { return e.description }
func (e *mockProjectionError) ErrorCode() string        { return e.code }
func (e *mockProjectionError) ErrorDescription() string { return e.description }
func (e *mockProjectionError) ErrorDiagnostics() []gafferruntime.Diagnostic {
	return nil
}

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

func TestClassifyError_RuntimeError(t *testing.T) {
	opts := `{"engineVersion":2}`
	session, err := gafferruntime.NewSession(`fromAll().when({
		BadEvent(s, e) { throw new Error("boom"); }
	})`, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	_, feedErr := session.Feed(testutil.Event("BadEvent", "s-1", 0))
	if feedErr == nil {
		t.Fatal("expected error")
	}

	fe := ClassifyError(feedErr)
	if fe.Code == "" {
		t.Error("expected non-empty code")
	}
	if fe.Description == "" {
		t.Error("expected non-empty description")
	}
	if fe.Code == "unexpected-error" {
		t.Errorf("expected a projection error code, got %q", fe.Code)
	}
}
