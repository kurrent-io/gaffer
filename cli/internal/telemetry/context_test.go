package telemetry

import (
	"context"
	"testing"
)

func TestClientFromContext_Absent(t *testing.T) {
	if got := ClientFromContext(context.Background()); got != nil {
		t.Errorf("ClientFromContext on empty ctx = %v, want nil", got)
	}
}

func TestWithClient_RoundTrip(t *testing.T) {
	c := New()
	ctx := WithClient(context.Background(), c)
	got := ClientFromContext(ctx)
	if got != c {
		t.Errorf("ClientFromContext = %p, want %p", got, c)
	}
}

func TestWithClient_NilStored(t *testing.T) {
	ctx := WithClient(context.Background(), nil)
	if got := ClientFromContext(ctx); got != nil {
		t.Errorf("ClientFromContext after WithClient(nil) = %v, want nil", got)
	}
}

func TestWithClient_ShadowsParent(t *testing.T) {
	outer := New()
	inner := New()
	ctx := WithClient(context.Background(), outer)
	ctx = WithClient(ctx, inner)
	if got := ClientFromContext(ctx); got != inner {
		t.Errorf("inner WithClient didn't shadow outer; got %p, want %p", got, inner)
	}
}
