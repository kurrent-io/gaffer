package gafferruntime

import (
	"sync"
	"testing"
	"time"
)

const debugOpts = `{"engineVersion":2,"debug":true}`

const debugTestEvent = `{"eventType":"ItemAdded","streamId":"stream-1","sequenceNumber":0,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-000000000000","created":"2026-01-01T00:00:00Z"}`

func TestDebug_BreakpointPausesAndContinues(t *testing.T) {
	opts := debugOpts
	source := "fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	var breakInfo BreakInfo
	var breakOnce sync.Once
	breakHit := make(chan struct{})

	session.OnBreak(func(info BreakInfo) {
		breakInfo = info
		breakOnce.Do(func() { close(breakHit) })
	})
	session.SetBreakpoint(4, 1, nil) //nolint:errcheck

	feedDone := make(chan error, 1)
	go func() {
		_, err := session.Feed(debugTestEvent)
		feedDone <- err
	}()

	select {
	case <-breakHit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for breakpoint")
	}

	if breakInfo.Reason != "breakpoint" {
		t.Fatalf("expected breakpoint reason, got %s", breakInfo.Reason)
	}
	if breakInfo.Line != 4 {
		t.Fatalf("expected line 4, got %d", breakInfo.Line)
	}

	session.Continue()

	select {
	case err := <-feedDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for feed to complete")
	}

	state, err := session.GetState(nil)
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}
	if state == nil || *state != `{"count":1}` {
		t.Fatalf("unexpected state: %v", state)
	}
}

func TestDebug_GetCallStack(t *testing.T) {
	opts := debugOpts
	source := "fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	breakHit := make(chan struct{}, 1)
	session.OnBreak(func(info BreakInfo) {
		breakHit <- struct{}{}
	})
	session.SetBreakpoint(4, 1, nil) //nolint:errcheck

	feedDone := make(chan struct{})
	go func() {
		_, _ = session.Feed(debugTestEvent)
		close(feedDone)
	}()

	select {
	case <-breakHit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for breakpoint")
	}

	frames, err := session.GetCallStack()
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 1 {
		t.Fatal("expected at least 1 frame")
	}
	if frames[0].Name != "ItemAdded" {
		t.Fatalf("expected ItemAdded, got %s", frames[0].Name)
	}

	session.Continue()
	<-feedDone
}

func TestDebug_GetScopesAndVariables(t *testing.T) {
	opts := debugOpts
	source := "fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	breakHit := make(chan struct{}, 1)
	session.OnBreak(func(info BreakInfo) {
		breakHit <- struct{}{}
	})
	session.SetBreakpoint(4, 1, nil) //nolint:errcheck

	feedDone := make(chan struct{})
	go func() {
		_, _ = session.Feed(debugTestEvent)
		close(feedDone)
	}()

	select {
	case <-breakHit:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for breakpoint")
	}

	scopes, err := session.GetScopes(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) < 1 {
		t.Fatal("expected at least 1 scope")
	}
	if scopes[0].VariablesReference <= 0 {
		t.Fatal("expected positive variablesReference")
	}

	vars, err := session.GetVariables(scopes[0].VariablesReference)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) < 1 {
		t.Fatal("expected at least 1 variable")
	}

	found := false
	for _, v := range vars {
		if v.Name == "s" {
			found = true
			if v.Type != "object" {
				t.Fatalf("expected object type for s, got %s", v.Type)
			}
		}
	}
	if !found {
		t.Fatal("expected to find variable 's'")
	}

	session.Continue()
	<-feedDone
}

func TestDebug_ClearBreakpoints(t *testing.T) {
	opts := debugOpts
	source := "fromAll().when({\n$init() { return { count: 0 }; },\nItemAdded(s, e) {\ns.count++;\nreturn s;\n}\n})"
	session, err := NewSession(source, &opts)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	session.SetBreakpoint(4, 1, nil) //nolint:errcheck
	session.ClearBreakpoints()

	result, err := session.Feed(debugTestEvent)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processed" {
		t.Fatalf("expected processed, got %s", result.Status)
	}
}
