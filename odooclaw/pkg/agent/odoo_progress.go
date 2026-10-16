package agent

import (
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"strings"
)

// Describe only the actual delegated tool and its model, without exposing args.
func odooToolActivity(tc providers.ToolCall, completed, failed bool) string {
	if tc.Name == odooDiscoverTool {
		if completed {
			if failed {
				return "Revisando las herramientas disponibles para esta tarea"
			}
			return "Herramientas preparadas para continuar"
		}
		return "Preparando las herramientas para tu solicitud"
	}
	model, _ := tc.Arguments["model"].(string)
	label := map[string]string{
		"res.partner": "los contactos", "sale.order": "los pedidos de venta",
		"sale.order.line": "las líneas de venta", "purchase.order": "los pedidos de compra",
		"stock.picking": "los albaranes", "stock.quant": "las existencias",
		"product.product": "los productos", "product.template": "los productos",
		"mrp.production": "las órdenes de fabricación", "mrp.workorder": "las órdenes de trabajo",
		"account.move": "las facturas", "hr.employee": "los empleados",
	}[model]
	if label == "" {
		label = "los datos solicitados"
	}
	name := strings.ToLower(tc.Name)
	verb, done := "Consultando", "Consulta realizada sobre"
	if strings.Contains(name, "search") || strings.Contains(name, "find") || strings.Contains(name, "lookup") {
		verb, done = "Buscando", "Búsqueda realizada sobre"
	}
	if strings.Contains(name, "create") {
		verb, done = "Creando", "Creación realizada sobre"
	}
	if strings.Contains(name, "write") || strings.Contains(name, "update") {
		verb, done = "Actualizando", "Actualización realizada sobre"
	}
	if strings.Contains(name, "unlink") || strings.Contains(name, "delete") {
		verb, done = "Eliminando", "Eliminación realizada sobre"
	}
	if completed {
		if failed {
			return "La operación sobre " + label + " ha dado un error; revisaré cómo continuar."
		}
		return done + " " + label + ". Revisando el resultado."
	}
	return verb + " " + label + " en Odoo"
}
