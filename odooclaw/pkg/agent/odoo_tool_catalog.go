package agent

import (
	"encoding/json"
	"strings"

	"github.com/nicolasramos/odooclaw/pkg/providers"
	"github.com/nicolasramos/odooclaw/pkg/tools"
)

const odooDiscoverTool = "odooclaw_discover_tools"

// This request-local catalog changes presentation, never execution authorization.
// Every original MCP tool stays registered and callable through its native guard.
type odooToolCatalog struct {
	loaded      map[string]bool
	all         []providers.ToolDefinition
	discoveries int
}

func newOdooToolCatalog() *odooToolCatalog {
	return &odooToolCatalog{loaded: map[string]bool{}}
}

func (c *odooToolCatalog) definitions(all []providers.ToolDefinition, messages []providers.Message) []providers.ToolDefinition {
	c.all = all
	core := []string{"odoo_query_records", "odoo_filter_text", "odoo_sample_records", "odoo_batch_lookup", "odoo_current_user", "odoo_read", "odoo_search_read", "odoo_create", "odoo_write", "odoo_post_chatter_message", "odoo_invoke_action", "odoo_get_model_schema", "odoo_unlink", "odoo_execute", "odoo_read_attachment", "odoo_get_model_permissions"}
	for _, t := range all {
		for _, name := range core {
			if strings.HasSuffix(t.Function.Name, "_"+name) {
				c.loaded[t.Function.Name] = true
			}
		}
	}
	// Retain tools used earlier in this conversation or directly requested in this turn.
	for _, m := range messages {
		for _, call := range m.ToolCalls {
			name := call.Name
			if name == "" && call.Function != nil {
				name = call.Function.Name
			}
			c.loaded[name] = true
		}
	}
	for _, t := range retrieveRelevantTools(all, lastUserMessageText(messages), 8) {
		c.loaded[t.Function.Name] = true
	}
	result := make([]providers.ToolDefinition, 0, len(all)+1)
	catalog := []map[string]string{}
	for _, t := range all {
		if c.loaded[t.Function.Name] {
			result = append(result, t)
			continue
		}
		description := strings.Join(strings.Fields(t.Function.Description), " ")
		runes := []rune(description)
		if len(runes) > 180 {
			description = string(runes[:180]) + "…"
		}
		catalog = append(catalog, map[string]string{"name": t.Function.Name, "description": description})
	}
	if len(catalog) == 0 {
		return result
	}
	catalogJSON, _ := json.Marshal(catalog)
	result = append(result, providers.ToolDefinition{Type: "function", Function: providers.ToolFunctionDefinition{
		Name:        odooDiscoverTool,
		Description: "All catalog tools remain available. This local operation loads their COMPLETE parameter schemas for the next step; it does not read or change Odoo records. Before using a catalog tool whose schema is not loaded, request its exact name here. Never claim a tool is unavailable because it is in this catalog. Use load_all=true if unsure or a task requires many tools. Generic Odoo tools already cover any permitted model. Catalog: " + string(catalogJSON),
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"names":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact tool names from the catalog."},
			"query":    map[string]any{"type": "string", "description": "Optional task description to discover relevant tools."},
			"load_all": map[string]any{"type": "boolean", "description": "Load all original tools and their full schemas, without changing permissions."},
		}, "additionalProperties": false},
	}})
	return result
}

func (c *odooToolCatalog) expandAll() {
	for _, definition := range c.all {
		c.loaded[definition.Function.Name] = true
	}
}

func (c *odooToolCatalog) discover(args map[string]any) *tools.ToolResult {
	c.discoveries++
	names := []string{}
	if raw, ok := args["names"]; ok {
		switch list := raw.(type) {
		case []any:
			for _, v := range list {
				s, ok := v.(string)
				if !ok {
					return tools.ErrorResult("names must contain exact catalog tool names")
				}
				names = append(names, s)
			}
		case []string:
			names = list
		default:
			return tools.ErrorResult("names must be an array")
		}
	}
	loadAll, _ := args["load_all"].(bool)
	query, _ := args["query"].(string)
	available := map[string]bool{}
	for _, t := range c.all {
		available[t.Function.Name] = true
	}
	for _, name := range names {
		if !available[name] {
			return tools.ErrorResult("Unknown catalog name; use an exact listed name or load_all=true. No Odoo operation was performed.")
		}
	}
	if loadAll || c.discoveries >= 4 || (len(names) == 0 && strings.TrimSpace(query) == "") {
		names = nil
		for _, t := range c.all {
			names = append(names, t.Function.Name)
		}
	} else if query != "" {
		for _, t := range retrieveRelevantTools(c.all, query, 12) {
			names = append(names, t.Function.Name)
		}
	}
	for _, name := range names {
		c.loaded[name] = true
	}
	result, _ := json.Marshal(map[string]any{"loaded_tools": names, "schemas_available_next_step": true, "odoo_operation_performed": false})
	return tools.SilentResult(string(result))
}

// Native denials are authoritative; exposing more tools cannot grant access.
func isOdooPermissionDenial(result *tools.ToolResult) bool {
	if result == nil || !result.IsError {
		return false
	}
	content := strings.ToLower(result.ForLLM)
	return strings.Contains(content, "permission denied") || strings.Contains(content, "accesserror")
}
