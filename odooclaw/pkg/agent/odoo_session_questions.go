package agent

import (
	"regexp"
	"strings"
	"unicode"
)

type odooSessionQuestion struct{ kind, model string }

// Whole-clause matching: never discard unknown clauses or use substring intent
// detection. A single action, qualifier or unknown question rejects the ENTIRE
// local answer, so the normal agent sees the original request and its context.
var odooQuestionSeparator = regexp.MustCompile(`\s+(?:y|e|ademas|tambien)(?:\s+(?:ademas|tambien))?\s+|[?!.,;]+`)

var odooQuestionAliases = map[string][]string{
	"greeting": {"buenos dias", "buenas tardes", "buenas noches", "hola", "buenas"},
	"identity": {
		"quien soy", "que usuario soy", "cual es mi usuario", "mi usuario", "mi cuenta", "mi perfil", "mi identidad",
		"con que usuario estoy", "con que usuario estoy conectado", "con que usuario estoy conectada",
		"con que cuenta estoy", "con que cuenta estoy conectado", "con que cuenta estoy conectada",
		"que usuario tengo", "que cuenta estoy usando", "con que usuario he iniciado sesion",
		"cual es mi nombre", "como me llamo", "cual es mi login", "mi login", "cual es mi id de usuario",
		"que perfil tengo seleccionado", "cual es mi perfil actual", "que operario soy", "cual es el operario actual",
	},
	"permissions": {
		"que permisos tengo", "cuales son mis permisos", "mis permisos", "que permisos tiene mi usuario", "que permisos tiene mi cuenta",
		"que permisos tengo en odoo", "cuales son mis permisos en odoo", "cuales son mis permisos actuales", "que permisos tengo actualmente",
		"que permisos tiene mi usuario actual", "que permisos tiene mi cuenta actual", "los permisos de mi cuenta", "los permisos de mi usuario",
	},
	"company": {
		"cual es mi empresa", "mi empresa", "mi empresa actual", "mi compañia", "mi compañia actual",
		"en que empresa estoy", "en que empresa estoy conectado", "en que empresa estoy conectada",
		"cual es mi compañia", "en que compañia estoy", "cual es la empresa activa", "cual es mi empresa actual",
		"que empresa tengo seleccionada", "cual es la compañia activa", "que compañia tengo seleccionada",
	},
	"settings": {
		"que modelo tengo seleccionado", "que modelo esta seleccionado", "cual es el modelo de este chat", "que modelo usa este chat",
		"que modelo estoy usando", "que modelo estas usando", "cual es mi modelo", "mi modelo", "mi modelo actual", "el modelo de este chat",
		"que modelo tengo", "cual es el modelo actual", "que modelo de ia estoy usando", "que modelo de ia tengo seleccionado",
		"que razonamiento tengo seleccionado", "que razonamiento estoy usando", "cual es mi nivel de razonamiento", "mi razonamiento",
		"que nivel de razonamiento tengo", "que razonamiento tiene este chat", "cual es el razonamiento de este chat",
		"que rapidez tengo seleccionada", "que servicio tengo seleccionado", "que service tier tengo", "mi service tier",
		"que prioridad tengo seleccionada", "cual es el servicio de este chat", "la configuracion del modelo", "cual es la configuracion del modelo",
	},
}

var odooPermissionTargets = map[string]string{
	"contactos": "res.partner", "pedidos de venta": "sale.order", "lineas de pedidos de venta": "sale.order.line",
	"pedidos de compra": "purchase.order", "ordenes de fabricacion": "mrp.production", "ordenes de trabajo": "mrp.workorder",
	"transferencias de inventario": "stock.picking", "facturas": "account.move", "asientos contables": "account.move",
	"contratos": "contract.contract", "lineas de contratos": "contract.line", "alertas de calidad": "quality.alert",
	"usuarios": "res.users", "grupos": "res.groups", "permisos de acceso": "ir.model.access", "reglas de registros": "ir.rule",
	"archivos adjuntos": "ir.attachment", "adjuntos": "ir.attachment",
}

func normalizeOdooQuestion(text string) string {
	text = strings.Map(func(r rune) rune {
		switch unicode.ToLower(r) {
		case 'á':
			return 'a'
		case 'é':
			return 'e'
		case 'í':
			return 'i'
		case 'ó':
			return 'o'
		case 'ú', 'ü':
			return 'u'
		case '¿', '¡':
			return ' '
		case '\n', '\r':
			return ';'
		default:
			return unicode.ToLower(r)
		}
	}, text)
	// Explicit harmless spelling variants only; no fuzzy matching of actions.
	words := strings.Fields(text)
	for i, word := range words {
		switch word {
		case "permisso", "permissos", "permisoss":
			words[i] = "permisos"
		}
	}
	return strings.Join(words, " ")
}

func trimOdooQuestionPoliteness(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSpace(strings.TrimSuffix(text, " por favor"))
	for _, prefix := range []string{"por favor ", "me puedes decir ", "puedes decirme ", "quiero saber ", "dime ", "muestrame ", "indicame "} {
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return text
}

func parseOdooSessionQuestions(text string) ([]odooSessionQuestion, bool) {
	if len(text) > 4096 {
		return nil, false
	}
	// Normalize again per clause below to cover spelling variants next to '?'.
	text = normalizeOdooQuestion(text)
	clauses := odooQuestionSeparator.Split(text, -1)
	if len(clauses) > 24 {
		return nil, false
	}
	var result []odooSessionQuestion
	seen := map[odooSessionQuestion]bool{}
	appendQuestion := func(q odooSessionQuestion) {
		if !seen[q] {
			result = append(result, q)
			seen[q] = true
		}
	}
	for _, clause := range clauses {
		clause = trimOdooQuestionPoliteness(clause)
		if clause == "" || clause == "por favor" {
			continue
		}
		clause = normalizeOdooQuestion(clause)
		if question, ok := matchOdooSessionQuestion(clause); ok {
			appendQuestion(question)
			continue
		}
		// Greeting without punctuation, e.g. "hola dime quién soy".
		for _, greeting := range odooQuestionAliases["greeting"] {
			if strings.HasPrefix(clause, greeting+" ") {
				appendQuestion(odooSessionQuestion{kind: "greeting"})
				clause = trimOdooQuestionPoliteness(strings.TrimPrefix(clause, greeting+" "))
				break
			}
		}
		clause = normalizeOdooQuestion(clause)
		question, ok := matchOdooSessionQuestion(clause)
		if !ok {
			return nil, false
		}
		appendQuestion(question)
	}
	return result, len(result) > 0
}

func matchOdooSessionQuestion(clause string) (odooSessionQuestion, bool) {
	for kind, aliases := range odooQuestionAliases {
		for _, alias := range aliases {
			if clause == alias {
				return odooSessionQuestion{kind: kind}, true
			}
		}
	}
	// General model ACLs only: never accept IDs, names, "este contacto", document
	// qualifiers or actual create/edit/delete imperatives. The reply always states
	// these are GENERAL rights subject to record rules and business validation.
	for target, model := range odooPermissionTargets {
		for _, prefix := range []string{"que permisos tengo sobre ", "que permisos tengo en ", "mis permisos sobre ", "mis permisos en ", "los permisos de mi cuenta sobre "} {
			if clause == prefix+target {
				return odooSessionQuestion{kind: "permissions", model: model}, true
			}
		}
		for _, operation := range []string{"consultar", "leer", "crear", "modificar", "editar", "eliminar", "borrar"} {
			if clause == "puedo "+operation+" "+target {
				return odooSessionQuestion{kind: "permissions", model: model}, true
			}
		}
	}
	return odooSessionQuestion{}, false
}
