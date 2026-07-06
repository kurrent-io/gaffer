//go:build integration

package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// acceptingReq is a request whose session accepts every elicitation - the
// fake human saying yes.
func acceptingReq(t *testing.T) *mcp.CallToolRequest {
	t.Helper()
	return gateReq(elicitSession(t, func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "accept"}, nil
	}))
}

// decliningReq is a request whose session declines every elicitation.
func decliningReq(t *testing.T) *mcp.CallToolRequest {
	t.Helper()
	return gateReq(elicitSession(t, func(_ context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		return &mcp.ElicitResult{Action: "decline"}, nil
	}))
}

// callVerb invokes a verb handler and decodes its success envelope.
func callVerb[In any](t *testing.T, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error), req *mcp.CallToolRequest, in In) map[string]any {
	t.Helper()
	result, _, err := handler(context.Background(), req, in)
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", testutil.MustType[*mcp.TextContent](t, result.Content[0]).Text)
	}
	var out map[string]any
	text := testutil.MustType[*mcp.TextContent](t, result.Content[0]).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	return out
}

func TestIntegration_OperateVerbs(t *testing.T) {
	s, client, name := setupDeployToolsProject(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Deploy the local definition for real (out of band, with gaffer's
	// engine version) so recreate/rollback have something to operate on.
	if err := client.Create(ctx, name, "fromAll().when({ $init() { return { v: 0 }; } })", remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatal(err)
	}
	deleted := false
	t.Cleanup(func() {
		if deleted {
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		_ = client.Disable(cctx, name)
		_ = client.Delete(cctx, name, remote.DeleteOptions{})
	})

	// Lifecycle verbs off production: no elicit fires, so a nil request is
	// enough - the client's tool-approval layer is the gate.
	res := callVerb(t, s.handleDeployPause, nil, operateInput{Name: name})
	if res["outcome"] != "disabled" || res["env"] != "default" || res["production"] != false {
		t.Fatalf("pause = %v", res)
	}
	res = callVerb(t, s.handleDeployResume, nil, operateInput{Name: name})
	if res["outcome"] != "enabled" {
		t.Fatalf("resume = %v", res)
	}
	res = callVerb(t, s.handleDeployAbort, nil, operateInput{Name: name})
	if res["outcome"] != "aborted" {
		t.Fatalf("abort = %v", res)
	}

	// Recreate always elicits: declined leaves the projection in place,
	// accepted rebuilds it with the recreate stamped in its history.
	result, _, err := s.handleDeployRecreate(context.Background(), decliningReq(t), deployRecreateInput{Name: name})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("declined recreate should refuse, got %v err %v", result, err)
	}
	if exists, _ := client.Exists(ctx, name); !exists {
		t.Fatal("declined recreate must leave the projection in place")
	}

	res = callVerb(t, s.handleDeployRecreate, acceptingReq(t), deployRecreateInput{Name: name})
	if res["outcome"] != "recreated" {
		t.Fatalf("recreate = %v", res)
	}
	hist := callTool(t, s, deployHistoryTool, s.handleDeployHistory, deployHistoryInput{Name: name})
	versions := hist["versions"].([]any)
	head := versions[0].(map[string]any)
	if head["kind"] != "recreate" || head["tool"] != remote.ToolName || head["operation"] != "recreate" {
		t.Fatalf("history head after recreate = %v, want a gaffer-stamped recreate", head)
	}
	recreatedHash := head["contentHash"].(string)

	// Out-of-band update creates a newer version; rollback (non-prod, no
	// elicit) returns to the recreated content by hash, stamping rollback.
	if err := client.Update(ctx, name, "fromAll().when({ $init() { return { v: 1 }; } })", remote.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	res = callVerb(t, s.handleDeployRollback, nil, deployRollbackInput{Name: name, Hash: recreatedHash[:8]})
	if res["outcome"] != "rolled-back" || res["hash"] != recreatedHash {
		t.Fatalf("rollback = %v", res)
	}
	res = callVerb(t, s.handleDeployRollback, nil, deployRollbackInput{Name: name, Hash: recreatedHash[:8]})
	if res["outcome"] != "unchanged" {
		t.Fatalf("second rollback = %v, want unchanged", res)
	}

	// Delete always elicits: a client without elicitation can't delete at
	// all, declined leaves it, accepted removes it.
	result, _, _ = s.handleDeployDelete(context.Background(), gateReq(elicitSession(t, nil)), deployDeleteInput{Name: name})
	if result == nil || !result.IsError {
		t.Fatal("delete without elicitation capability must refuse")
	}
	result, _, _ = s.handleDeployDelete(context.Background(), decliningReq(t), deployDeleteInput{Name: name})
	if result == nil || !result.IsError {
		t.Fatal("declined delete must refuse")
	}
	if exists, _ := client.Exists(ctx, name); !exists {
		t.Fatal("refused deletes must leave the projection in place")
	}
	res = callVerb(t, s.handleDeployDelete, acceptingReq(t), deployDeleteInput{Name: name})
	if res["outcome"] != "deleted" {
		t.Fatalf("delete = %v", res)
	}
	deleted = true
	if exists, _ := client.Exists(ctx, name); exists {
		t.Fatal("accepted delete should remove the projection")
	}
}
