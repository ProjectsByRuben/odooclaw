package agent

import (
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"strings"
	"testing"
)

func TestOdooToolActivityDescribesActionAndErrorWithoutArguments(t *testing.T) {
	tc := providers.ToolCall{Name: "mcp_odoo-manager_odoo_search", Arguments: map[string]any{"model": "res.partner", "domain": "PRIVATE_FILTER"}}
	if got := odooToolActivity(tc, false, false); got != "Buscando los contactos en Odoo" {
		t.Fatal(got)
	}
	if got := odooToolActivity(tc, true, false); !strings.Contains(got, "Búsqueda realizada sobre los contactos") || strings.Contains(got, "PRIVATE_FILTER") {
		t.Fatal(got)
	}
	if got := odooToolActivity(tc, true, true); !strings.Contains(got, "ha dado un error") || strings.Contains(got, "realizada") {
		t.Fatal(got)
	}
}
