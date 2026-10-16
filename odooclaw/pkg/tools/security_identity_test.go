package tools

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type securityShellTool struct{ called bool }

func (t *securityShellTool) Name() string               { return "exec" }
func (t *securityShellTool) Description() string        { return "shell" }
func (t *securityShellTool) Parameters() map[string]any { return map[string]any{} }
func (t *securityShellTool) Execute(context.Context, map[string]any) *ToolResult {
	t.called = true
	return &ToolResult{ForLLM: "executed"}
}

func TestSecurityRejectsAlternativeOdooTools(t *testing.T) {
	registry := NewToolRegistry()
	shell := &securityShellTool{}
	registry.Register(shell)
	result := registry.ExecuteWithContext(context.Background(), "exec", nil, "odoo", "mail.channel_55", "16", nil, nil)
	if !result.IsError || shell.called {
		t.Fatal("Odoo chat executed a non-delegated tool")
	}
}

func TestSecurityRejectsMissingOrExternalIdentity(t *testing.T) {
	var calls atomic.Int32
	manager := &MockMCPManager{callToolFunc: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{}, nil
	}}
	tool := NewMCPTool(manager, "odoo-manager", &mcp.Tool{Name: "odoo_search_read"})
	for _, test := range []struct{ channel, uid string }{{"odoo", ""}, {"odoo", "bad"}, {"odoo", "1"}, {"cli", "16"}, {"telegram", "16"}} {
		tool.SetMessageContext(test.channel, "chat", test.uid, nil)
		if !tool.Execute(context.Background(), map[string]any{"sender_id": 2}).IsError {
			t.Fatalf("accepted identity %#v", test)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("Invalid identity reached MCP")
	}
}

func TestSecurityConcurrentUsersCannotOverrideIdentity(t *testing.T) {
	registry := NewToolRegistry()
	var calls atomic.Int32
	manager := &MockMCPManager{callToolFunc: func(_ context.Context, _ string, _ string, args map[string]any) (*mcp.CallToolResult, error) {
		time.Sleep(time.Millisecond)
		if args["sender_id"] != args["expected_uid"] || args["request_token"] != fmt.Sprint(args["expected_uid"]) {
			return nil, fmt.Errorf("cross-user identity: %v", args)
		}
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	}}
	tool := NewMCPTool(manager, "odoo-manager", &mcp.Tool{Name: "odoo_search_read"})
	registry.Register(tool)
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		uid := 16 + n%2*6
		wg.Add(1)
		go func(uid int) {
			defer wg.Done()
			result := registry.ExecuteWithContext(context.Background(), tool.Name(), map[string]any{"sender_id": 2, "expected_uid": uid}, "odoo", "mail.channel_55", fmt.Sprint(uid), map[string]string{"request_token": fmt.Sprint(uid)}, nil)
			if result.IsError {
				t.Error(result.ForLLM)
			}
		}(uid)
	}
	wg.Wait()
	if calls.Load() != 20 {
		t.Fatalf("Only %d/20 requests retained the authenticated identity", calls.Load())
	}
}

func TestSecurityEmptyStructuredLookupIsNotLost(t *testing.T) {
	manager := &MockMCPManager{callToolFunc: func(context.Context, string, string, map[string]any) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{StructuredContent: map[string]any{"result": []any{}}}, nil
	}}
	tool := NewMCPTool(manager, "odoo-manager", &mcp.Tool{Name: "odoo_search_read", InputSchema: map[string]any{"properties": map[string]any{"request_token": map[string]any{"type": "string"}, "model": map[string]any{"type": "string"}}}})
	tool.SetMessageContext("odoo", "mail.channel_54", "2", map[string]string{"request_token": "secret-capability"})
	result := tool.Execute(context.Background(), map[string]any{"request_token": "forged"})
	if result.IsError || result.ForLLM != `{"result":[]}` {
		t.Fatalf("Lost empty result: %#v", result)
	}
	if _, exposed := tool.Parameters()["properties"].(map[string]any)["request_token"]; exposed {
		t.Fatal("Capability exposed to the model")
	}
}
