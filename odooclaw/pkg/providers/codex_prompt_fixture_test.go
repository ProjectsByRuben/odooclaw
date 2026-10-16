package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Export the actual provider payload for the optional real-CLI regression.
func TestExportCodexOptimizationPrompts(t *testing.T) {
	dir := os.Getenv("ODOOCLAW_PROMPT_FIXTURES")
	if dir == "" {
		t.Skip("real CLI fixtures not requested")
	}
	instructions := "You are the reasoning component of OdooClaw. Available tools are executed by the external runner after your final tool_calls response. Current Session: authenticated internal Odoo user ID 2, native access to res.partner read/create/write/unlink and ir.attachment read. Request an appropriate listed tool; do not report data or action success before its tool result. Do not use native shell tools."
	for _, mode := range []string{"full", "deferred"} {
		data, err := os.ReadFile(filepath.Join(dir, "odooclaw-"+mode+"-definitions.json"))
		if err != nil {
			t.Fatal(err)
		}
		var definitions []ToolDefinition
		if err = json.Unmarshal(data, &definitions); err != nil {
			t.Fatal(err)
		}
		prompt := (&CodexCliProvider{}).buildPrompt([]Message{{Role: "system", Content: instructions}, {Role: "user", Content: "Muestra tres contactos de Odoo con su nombre y correo."}}, definitions)
		if err = os.WriteFile(filepath.Join(dir, mode+"-contacts-prompt.txt"), []byte(prompt), 0600); err != nil {
			t.Fatal(err)
		}
		if mode == "deferred" {
			prompt = (&CodexCliProvider{}).buildPrompt([]Message{{Role: "system", Content: instructions}, {Role: "user", Content: "Lee el correo Outlook .msg de ir.attachment ID 424242 y lista los PDF adjuntos. Primero consulta su contenido con la herramienta disponible."}}, definitions)
			if err = os.WriteFile(filepath.Join(dir, "deferred-email-prompt.txt"), []byte(prompt), 0600); err != nil {
				t.Fatal(err)
			}
			// Locate one catalog tool whose complete schema is deferred.
			for _, d := range definitions {
				if d.Function.Name != "odooclaw_discover_tools" {
					continue
				}
				prompt = (&CodexCliProvider{}).buildPrompt([]Message{{Role: "system", Content: instructions}, {Role: "user", Content: "Necesito usar mcp_odoo-manager_odoo_get_purchase_receipt_status. Antes de ejecutarla, carga su definición completa desde el catálogo disponible."}}, definitions)
				if err = os.WriteFile(filepath.Join(dir, "deferred-discovery-prompt.txt"), []byte(prompt), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
