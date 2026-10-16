package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"github.com/nicolasramos/odooclaw/pkg/tools"
)

type catalogPipelineManager struct {
	called int
	deny   bool
	t      *testing.T
}

func (m *catalogPipelineManager) CallTool(_ context.Context, server, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	m.called++
	if server != "odoo-manager" || tool != "odoo_read_attachment" || fmt.Sprint(args["sender_id"]) != "16" || args["request_token"] != "real-request-capability" {
		m.t.Fatalf("identity changed: %v %s/%s", args, server, tool)
	}
	if m.deny {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Permission denied: the active Odoo user is not allowed to perform this operation."}}}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"format":"pdf","text":"MANDATORY_DETAIL_IN_DOCUMENT","untrusted_document_content":true}`}}}, nil
}

func TestOdooCatalogPipelinePreservesExecutionIdentityAndBudget(t *testing.T) {
	t.Setenv("ODOOCLAW_ODOO_DEFER_TOOLS", "true")
	dir := t.TempDir()
	script := `#!/bin/sh
n=0
[ ! -f "$CATALOG_TEST_DIR/step" ] || n=$(cat "$CATALOG_TEST_DIR/step")
cat > "$CATALOG_TEST_DIR/prompt_$n"
cat "$CATALOG_TEST_DIR/event_$n"
printf '%s' "$((n+1))" > "$CATALOG_TEST_DIR/step"
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_TEST_DIR", dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	outputs := []string{
		`{"tool_calls":[{"id":"discover","type":"function","function":{"name":"odooclaw_discover_tools","arguments":"{\"load_all\":true}"}}]}`,
		`{"tool_calls":[{"id":"pdf","type":"function","function":{"name":"mcp_odoo-manager_odoo_read_attachment","arguments":"{\"attachment_id\":10,\"sender_id\":2,\"request_token\":\"forged\"}"}}]}`,
		"MANDATORY_DETAIL_IN_DOCUMENT",
	}
	for i, output := range outputs {
		event, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": output}})
		event = append(event, []byte("\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":100,\"cached_input_tokens\":40,\"output_tokens\":20}}\n")...)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("event_%d", i)), event, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = dir
	cfg.Agents.Defaults.Model = "codex-cli"
	cfg.Agents.Defaults.MaxToolIterations = 2
	cfg.Tools.MCP.Enabled = false
	al := NewAgentLoop(cfg, bus.NewMessageBus(), providers.NewCodexCliProvider(dir))
	agent := al.registry.GetDefaultAgent()
	manager := &catalogPipelineManager{t: t}
	for _, definition := range catalogFixture() {
		schema := definition.Function.Parameters
		schema["properties"].(map[string]any)["sender_id"] = map[string]any{"type": "integer"}
		schema["properties"].(map[string]any)["request_token"] = map[string]any{"type": "string"}
		name := strings.TrimPrefix(definition.Function.Name, "mcp_odoo-manager_")
		agent.Tools.Register(tools.NewMCPTool(manager, "odoo-manager", &mcp.Tool{Name: name, Description: definition.Function.Description, InputSchema: schema}))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = withRequestUsage(ctx)
	result, iterations, err := al.runLLMIteration(ctx, agent, []providers.Message{{Role: "user", Content: "Lee el PDF adjunto"}}, processOptions{Channel: "odoo", ChatID: "mail.channel_10", SenderID: "16", SessionKey: "catalog-test", Metadata: map[string]string{"request_token": "real-request-capability"}})
	if err != nil || result != "MANDATORY_DETAIL_IN_DOCUMENT" || iterations != 2 || manager.called != 1 {
		t.Fatalf("regression: result=%q iterations=%d calls=%d error=%v", result, iterations, manager.called, err)
	}
	usage := usageSnapshot(ctx)
	if usage == nil || !usage.Complete || usage.TotalTokens != 360 || usage.CachedInputTokens != 120 || usage.ModelCalls != 3 {
		t.Fatalf("all model steps (including discovery) must be counted: %+v", usage)
	}
	initial, _ := os.ReadFile(filepath.Join(dir, "prompt_0"))
	full, _ := os.ReadFile(filepath.Join(dir, "prompt_1"))
	final, _ := os.ReadFile(filepath.Join(dir, "prompt_2"))
	if !strings.Contains(string(initial), odooDiscoverTool) || !strings.Contains(string(full), "important_required_field") || !strings.Contains(string(final), "MANDATORY_DETAIL_IN_DOCUMENT") {
		t.Fatal("schema discovery or document content lost")
	}
	os.Remove(filepath.Join(dir, "step"))
	agent.MaxIterations = 1
	manager.called = 0
	ctx = withRequestUsage(ctx)
	partial, steps, err := al.runLLMIteration(ctx, agent, []providers.Message{{Role: "user", Content: "Lee el PDF adjunto"}}, processOptions{Channel: "odoo", ChatID: "mail.channel_10", SenderID: "16", SessionKey: "budget-test", Metadata: map[string]string{"request_token": "real-request-capability"}})
	if err != nil || steps != 1 || manager.called != 1 || !strings.Contains(partial, "resultados obtenidos son parciales") || strings.Contains(partial, "config.json") {
		t.Fatalf("bad budget response: %q %d %v", partial, steps, err)
	}
	if usage := usageSnapshot(ctx); usage.TotalTokens != 240 || usage.ModelCalls != 2 || !usage.Complete {
		t.Fatalf("partial response must retain consumed model tokens: %+v", usage)
	}
	// A native denial needs no additional tool schemas and must not repeat the operation.
	os.Remove(filepath.Join(dir, "step"))
	manager.called = 0
	manager.deny = true
	agent.MaxIterations = 2
	deniedOutputs := []string{outputs[1], "No tienes permisos para leer este documento."}
	for i, output := range deniedOutputs {
		event, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": output}})
		event = append(event, []byte("\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":100,\"cached_input_tokens\":40,\"output_tokens\":20}}\n")...)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("event_%d", i)), event, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx = withRequestUsage(ctx)
	denied, _, err := al.runLLMIteration(ctx, agent, []providers.Message{{Role: "user", Content: "Lee el PDF adjunto"}}, processOptions{Channel: "odoo", ChatID: "mail.channel_10", SenderID: "16", SessionKey: "denied-test", Metadata: map[string]string{"request_token": "real-request-capability"}})
	deniedPrompt, _ := os.ReadFile(filepath.Join(dir, "prompt_1"))
	if err != nil || manager.called != 1 || !strings.Contains(denied, "permisos") || !strings.Contains(string(deniedPrompt), odooDiscoverTool) {
		t.Fatalf("denial expanded the full catalog or retried the native operation: %q calls=%d err=%v", denied, manager.called, err)
	}
	for _, prompt := range [][]byte{initial, full, final} {
		if strings.Contains(string(prompt), "real-request-capability") {
			t.Fatal("secret capability reached model")
		}
	}
}
