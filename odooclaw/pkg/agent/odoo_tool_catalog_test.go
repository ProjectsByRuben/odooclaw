package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"github.com/nicolasramos/odooclaw/pkg/tools"
)

func catalogFixture() []providers.ToolDefinition {
	all := []providers.ToolDefinition{}
	for _, name := range []string{"odoo_read_attachment", "odoo_create", "odoo_write", "odoo_unlink", "odoo_execute", "odoo_search_read", "odoo_get_model_schema", "odoo_get_model_permissions", "odoo_read", "odoo_batch_lookup", "odoo_current_user", "odoo_post_chatter_message", "odoo_invoke_action", "odoo_make_special_order", "odoo_special_payroll", "odoo_special_manufacturing"} {
		all = append(all, providers.ToolDefinition{Type: "function", Function: providers.ToolFunctionDefinition{Name: "mcp_odoo-manager_" + name, Description: "Execute " + name, Parameters: map[string]any{"type": "object", "properties": map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}, "important_required_field": map[string]any{"type": "string"}}, "required": []string{"important_required_field"}}}})
	}
	return all
}
func TestOdooCatalogPreservesCoreAndExactSchemas(t *testing.T) {
	all := catalogFixture()
	c := newOdooToolCatalog()
	defs := c.definitions(all, nil)
	byName := map[string]providers.ToolDefinition{}
	for _, d := range defs {
		byName[d.Function.Name] = d
	}
	for _, original := range all[:13] {
		originalJSON, _ := json.Marshal(original)
		actualJSON, _ := json.Marshal(byName[original.Function.Name])
		if string(originalJSON) != string(actualJSON) {
			t.Fatalf("core schema changed: %s", original.Function.Name)
		}
	}
	c.loaded = map[string]bool{}
	// Even a rarely used specialized action remains discoverable.
	result := c.discover(map[string]any{"names": []any{all[14].Function.Name}})
	if result.IsError {
		t.Fatal(result.ForLLM)
	}
	defs = c.definitions(all, nil)
	found := false
	for _, d := range defs {
		if d.Function.Name == all[14].Function.Name {
			b, _ := json.Marshal(d)
			a, _ := json.Marshal(all[14])
			if string(a) != string(b) {
				t.Fatal("schema altered")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("specialized tool lost")
	}
}
func TestOdooCatalogLoadAllAndRequestIsolation(t *testing.T) {
	all := catalogFixture()
	a, b := newOdooToolCatalog(), newOdooToolCatalog()
	a.definitions(all, nil)
	b.definitions(all, nil)
	a.discover(map[string]any{"load_all": true})
	if len(a.definitions(all, nil)) != len(all) {
		t.Fatal("full catalog fallback failed")
	}
	if len(b.loaded) >= len(all) {
		t.Fatal("discovery leaked between requests")
	}
	before := len(b.loaded)
	bad := b.discover(map[string]any{"names": []any{"exec", "sudo", "another_server_private_tool"}})
	if !bad.IsError || len(b.loaded) != before {
		t.Fatal("unknown tool admitted")
	}
	for i := 0; i < 4; i++ {
		b.discover(map[string]any{"query": "arbitrary"})
	}
	if len(b.definitions(all, nil)) != len(all) {
		t.Fatal("discovery did not converge to all tools")
	}
}
func TestOdooCatalogRestoresPreviouslyUsedTool(t *testing.T) {
	all := catalogFixture()
	c := newOdooToolCatalog()
	history := []providers.Message{{Role: "assistant", ToolCalls: []providers.ToolCall{{Function: &providers.FunctionCall{Name: all[15].Function.Name}}}}}
	found := false
	for _, d := range c.definitions(all, history) {
		if d.Function.Name == all[15].Function.Name {
			found = true
		}
	}
	if !found {
		t.Fatal("conversation lost an earlier tool")
	}
}
func TestOdooCatalogActualInventory(t *testing.T) {
	path := os.Getenv("ODOOCLAW_CATALOG_FIXTURE")
	if path == "" {
		t.Skip("live inventory fixture not supplied")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var all []providers.ToolDefinition
	if err = json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	for i, d := range all {
		tool := tools.NewMCPTool(nil, "odoo-manager", &mcp.Tool{Name: strings.TrimPrefix(d.Function.Name, "mcp_odoo-manager_"), Description: d.Function.Description, InputSchema: d.Function.Parameters})
		all[i] = providers.ToolDefinition{Type: "function", Function: providers.ToolFunctionDefinition{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()}}
	}
	if output := os.Getenv("ODOOCLAW_CATALOG_FULL_OUTPUT"); output != "" {
		data, _ := json.Marshal(all)
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := newOdooToolCatalog()
	defs := c.definitions(all, []providers.Message{{Role: "user", Content: "muestra tres contactos"}})
	if output := os.Getenv("ODOOCLAW_CATALOG_OUTPUT"); output != "" {
		data, _ := json.Marshal(defs)
		if err = os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	encodedAll, _ := json.Marshal(all)
	encodedDeferred, _ := json.Marshal(defs)
	if len(encodedDeferred) >= len(encodedAll) {
		t.Fatal("catalog grew instead of shrinking")
	}
	serialized := string(encodedDeferred)
	// Every tool name must either have its schema loaded or occur in the catalog.
	for _, original := range all {
		if !strings.Contains(serialized, original.Function.Name) {
			t.Fatalf("missing tool %s", original.Function.Name)
		}
	}
	t.Logf("Full catalog: %d tools / %d bytes; deferred catalog: %d full definitions / %d bytes; reduction %.1f%%", len(all), len(encodedAll), len(defs), len(encodedDeferred), 100*(1-float64(len(encodedDeferred))/float64(len(encodedAll))))
	c.discover(map[string]any{"load_all": true})
	actual, _ := json.Marshal(c.definitions(all, nil))
	if string(actual) != string(encodedAll) {
		t.Fatal("full catalog restoration changed the definitions")
	}
}

func TestOdooCatalogFailureFallbackRestoresOriginalCatalog(t *testing.T) {
	c := newOdooToolCatalog()
	all := catalogFixture()
	c.definitions(all, nil)
	c.expandAll()
	actual, _ := json.Marshal(c.definitions(all, nil))
	expected, _ := json.Marshal(all)
	if string(actual) != string(expected) {
		t.Fatal("failure fallback did not restore original catalog")
	}
}

func TestNativePermissionDenialDoesNotNeedCatalogExpansion(t *testing.T) {
	if !isOdooPermissionDenial(tools.ErrorResult("MCP tool returned error: Permission denied: active user cannot create")) {
		t.Fatal("native permission denial should not cause full catalog expansion")
	}
	if isOdooPermissionDenial(tools.ErrorResult("Unknown tool: missing schema")) {
		t.Fatal("schema errors must retain discovery recovery")
	}
	if isOdooPermissionDenial(tools.SilentResult("permission denied mentioned in a document")) {
		t.Fatal("successful document content must not be classified as an operation denial")
	}
}
