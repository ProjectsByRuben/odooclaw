package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Composable questions about authenticated session facts only. Requests with
// business work, media or missing/mismatched facts keep the normal agent path.
func odooSessionReply(opts processOptions) (string, bool) {
	if opts.Channel != "odoo" || len(opts.Media) > 0 || opts.Metadata["model"] != "mail.channel" {
		return "", false
	}
	var info struct {
		User struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Login    string `json:"login"`
			Company  string `json:"company_name"`
			Timezone string `json:"timezone"`
		} `json:"user"`
		Permissions map[string]odooSessionACL `json:"model_permissions"`
	}
	if json.Unmarshal([]byte(opts.Metadata["session_info"]), &info) != nil {
		return "", false
	}
	uid, err := strconv.Atoi(opts.SenderID)
	if err != nil || uid <= 1 || info.User.ID != uid || info.User.Name == "" || info.User.Login == "" || info.User.Company == "" {
		return "", false
	}
	if reply, ok := odooUtilityReply(opts.UserMessage, info.User.Timezone, time.Now()); ok {
		return reply, true
	}
	questions, ok := parseOdooSessionQuestions(opts.UserMessage)
	if !ok {
		return "", false
	}

	display := opts.Metadata["sender_name"]
	if strings.TrimSpace(display) == "" {
		display = info.User.Name
	}
	var parts []string
	for _, question := range questions {
		var reply string
		switch question.kind {
		case "permissions":
			areas := odooPermissionAreas
			if question.model != "" {
				areas = nil
				for _, area := range odooPermissionAreas {
					if area.model == question.model {
						areas = append(areas, area)
					}
				}
				if len(areas) != 1 {
					return "", false
				}
			}
			var verified bool
			reply, verified = odooPermissionReplyForAreas(info.User.Name, info.User.Company, info.Permissions, areas)
			if !verified {
				return "", false
			}
		case "company":
			reply = fmt.Sprintf("La empresa activa de tu sesión de Odoo es **%s**.", escapeSessionMarkdown(info.User.Company))
		case "settings":
			var settings struct {
				Model  string `json:"model"`
				Effort string `json:"reasoning_effort"`
				Tier   string `json:"service_tier"`
			}
			if json.Unmarshal([]byte(opts.Metadata["model_settings"]), &settings) != nil || strings.TrimSpace(settings.Model) == "" {
				return "", false
			}
			// No inferred global defaults when request metadata omits a setting.
			efforts := map[string]string{"none": "sin razonamiento", "minimal": "mínimo", "low": "bajo", "medium": "medio", "high": "alto", "xhigh": "extra alto", "max": "máximo", "ultra": "ultra"}
			tiers := map[string]string{"default": "estándar", "priority": "prioritario"}
			effort, effortOK := efforts[settings.Effort]
			tier, tierOK := tiers[settings.Tier]
			if !effortOK || !tierOK {
				return "", false
			}
			reply = fmt.Sprintf("Este chat tiene configurado **%s**, razonamiento **%s** y servicio **%s**. Esta respuesta usa los datos de configuración y no necesita consultar al modelo.", escapeSessionMarkdown(settings.Model), effort, tier)
		case "greeting":
			reply = fmt.Sprintf("Hola, %s.", escapeSessionMarkdown(display))
			if len(questions) == 1 {
				reply += " ¿En qué puedo ayudarte?"
			}
		case "identity":
			reply = fmt.Sprintf("Estás conectado con la cuenta de Odoo **%s** (acceso **%s**, ID %d), en **%s**.", escapeSessionMarkdown(info.User.Name), escapeSessionMarkdown(info.User.Login), uid, escapeSessionMarkdown(info.User.Company))
			if display != info.User.Name {
				reply = "Tu perfil actual es **" + escapeSessionMarkdown(display) + "**.\n\n" + reply
			}
			reply += "\n\nLas acciones se ejecutan con los permisos de esta cuenta de Odoo."
		default:
			return "", false
		}
		parts = append(parts, reply)
	}
	return strings.Join(parts, "\n\n"), true
}

func escapeSessionMarkdown(value string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ").Replace(value)
}

// Pointers distinguish denied rights (false) from absent/unverified metadata.
type odooSessionACL struct {
	Available *bool `json:"available"`
	Read      *bool `json:"read"`
	Create    *bool `json:"create"`
	Write     *bool `json:"write"`
	Unlink    *bool `json:"unlink"`
}

type odooPermissionArea struct{ model, label string }

var odooPermissionAreas = []odooPermissionArea{
	{"res.partner", "Contactos"}, {"sale.order", "Pedidos de venta"},
	{"sale.order.line", "Líneas de pedidos de venta"}, {"purchase.order", "Pedidos de compra"},
	{"mrp.production", "Órdenes de fabricación"}, {"mrp.workorder", "Órdenes de trabajo"},
	{"stock.picking", "Transferencias de inventario"}, {"account.move", "Facturas y asientos"},
	{"contract.contract", "Contratos"}, {"contract.line", "Líneas de contratos"},
	{"quality.alert", "Alertas de calidad"}, {"res.users", "Usuarios"},
	{"res.groups", "Grupos"}, {"ir.model.access", "Permisos de acceso"},
	{"ir.rule", "Reglas de registros"}, {"ir.attachment", "Archivos adjuntos"},
}

func odooPermissionReplyForAreas(name, company string, permissions map[string]odooSessionACL, areas []odooPermissionArea) (string, bool) {
	var rows strings.Builder
	for _, area := range areas {
		acl, exists := permissions[area.model]
		if !exists || acl.Available == nil {
			return "", false
		}
		rights := "Módulo no disponible"
		if *acl.Available {
			operations := []struct {
				allowed *bool
				label   string
			}{
				{acl.Read, "Consultar"}, {acl.Create, "Crear"}, {acl.Write, "Modificar"}, {acl.Unlink, "Eliminar"},
			}
			var allowed []string
			for _, op := range operations {
				if op.allowed == nil {
					return "", false
				}
				if *op.allowed {
					allowed = append(allowed, op.label)
				}
			}
			rights = "Sin permisos generales"
			if len(allowed) > 0 {
				rights = strings.Join(allowed, ", ")
			}
		}
		fmt.Fprintf(&rows, "| %s | %s |\n", area.label, rights)
	}
	return fmt.Sprintf("Permisos generales de la cuenta **%s** en **%s**, verificados por Odoo:\n\n| Área | Permisos generales |\n| --- | --- |\n%s\nEste resumen cubre las áreas indicadas. Las reglas de registros, compañías, campos y validaciones de negocio pueden limitar cada operación. Odoo comprueba el acceso al ejecutarla; el perfil del operario no amplía estos permisos.", escapeSessionMarkdown(name), escapeSessionMarkdown(company), rows.String()), true
}
