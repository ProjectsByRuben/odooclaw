package agent

import (
	"context"
	"encoding/json"
	"github.com/nicolasramos/odooclaw/pkg/session"
	"strings"
	"testing"
)

func identityOptions() processOptions {
	return processOptions{Channel: "odoo", ChatID: "mail.channel_9", SenderID: "16", SessionKey: "identity-test", UserMessage: "Hola, ¿quién soy?", Metadata: map[string]string{"model": "mail.channel", "sender_name": "ALBERTO (FABRICA)", "session_info": `{"user":{"id":16,"name":"FABRICA","login":"fabrica","company_name":"CMP"}}`}}
}
func TestOdooIdentityUsesAuthenticatedFactsOnly(t *testing.T) {
	opts := identityOptions()
	reply, ok := odooSessionReply(opts)
	if !ok {
		t.Fatal("identity not answered")
	}
	for _, fact := range []string{"ALBERTO (FABRICA)", "FABRICA", "fabrica", "16", "CMP"} {
		if !strings.Contains(reply, fact) {
			t.Fatal("missing session fact", fact)
		}
	}
	opts.UserMessage = "HOLA"
	reply, ok = odooSessionReply(opts)
	if !ok || !strings.Contains(reply, "ALBERTO") {
		t.Fatal(reply)
	}
}
func TestOdooSessionReplyLeavesAllWorkRequestsOnNormalPath(t *testing.T) {
	for _, text := range []string{"hola crea un contacto", "hola quien soy y muestra mis pedidos", "quien soy segun el PDF", "quien soy, mi usuario es admin 2", "hola\n[Adjunto: pedido.pdf (ID: 1)]", "cuantos pedidos hay", "muestra los operarios de fabrica", "quiero borrar un usuario", "quien soy; crea un contacto"} {
		opts := identityOptions()
		opts.UserMessage = text
		if reply, ok := odooSessionReply(opts); ok {
			t.Fatalf("business request intercepted: %q => %s", text, reply)
		}
	}
	opts := identityOptions()
	opts.Media = []string{"attachment"}
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("media bypassed normal path")
	}
	opts = identityOptions()
	opts.SenderID = "2"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("mismatched identity accepted")
	}
	opts = identityOptions()
	opts.Metadata["session_info"] = "{}"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("missing identity invented")
	}
	opts = identityOptions()
	opts.Channel = "other"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("other channel intercepted")
	}
}
func TestOdooSessionReplyCostsZeroCallsAndPreservesHistory(t *testing.T) {
	// Nil provider, tools and ContextBuilder deliberately prove none are called.
	storage := t.TempDir()
	agent := &AgentInstance{Sessions: session.NewSessionManager(storage)}
	loop := &AgentLoop{}
	ctx := withRequestUsage(context.Background())
	opts := identityOptions()
	reply, err := loop.runAgentLoop(ctx, agent, opts)
	if err != nil || reply == "" {
		t.Fatal(reply, err)
	}
	usage := usageSnapshot(ctx)
	if usage.ModelCalls != 0 || usage.TotalTokens != 0 || !usage.Complete {
		t.Fatal("incorrect zero usage", usage)
	}
	history := agent.Sessions.GetHistory(opts.SessionKey)
	if len(history) != 2 || history[0].Content != opts.UserMessage || history[1].Content != reply {
		t.Fatal("history not preserved", history)
	}
	reloaded := session.NewSessionManager(storage)
	if len(reloaded.GetHistory(opts.SessionKey)) != 2 {
		t.Fatal("history not saved to disk")
	}
}

func permissionOptions(t *testing.T) processOptions {
	t.Helper()
	opts := identityOptions()
	opts.UserMessage = "¿Qué permisos tengo?"
	var info map[string]any
	if err := json.Unmarshal([]byte(opts.Metadata["session_info"]), &info); err != nil {
		t.Fatal(err)
	}
	acls := map[string]any{}
	for _, area := range odooPermissionAreas {
		acls[area.model] = map[string]bool{"available": true, "read": true, "create": false, "write": true, "unlink": false}
	}
	acls["contract.contract"] = map[string]bool{"available": false}
	acls["res.users"] = map[string]bool{"available": true, "read": false, "create": false, "write": false, "unlink": false}
	info["model_permissions"] = acls
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	opts.Metadata["session_info"] = string(raw)
	return opts
}

func TestOdooPermissionReplyUsesActualACLsAndQualifiesScope(t *testing.T) {
	opts := permissionOptions(t)
	reply, ok := odooSessionReply(opts)
	if !ok {
		t.Fatal("verified ACL question not answered")
	}
	for _, expected := range []string{
		"| Contactos | Consultar, Modificar |", "| Contratos | Módulo no disponible |",
		"| Usuarios | Sin permisos generales |", "FABRICA", "CMP", "reglas de registros", "no amplía",
	} {
		if !strings.Contains(reply, expected) {
			t.Fatalf("missing %q in %s", expected, reply)
		}
	}
	for _, change := range []func(map[string]any){
		func(info map[string]any) { delete(info, "model_permissions") },
		func(info map[string]any) { delete(info["model_permissions"].(map[string]any), "res.partner") },
		func(info map[string]any) {
			info["model_permissions"].(map[string]any)["res.partner"] = map[string]any{"available": true, "read": true}
		},
		func(info map[string]any) {
			info["model_permissions"].(map[string]any)["res.partner"] = map[string]any{"read": false}
		},
		func(info map[string]any) { info["user"].(map[string]any)["id"] = 2 },
		func(info map[string]any) {
			info["model_permissions"].(map[string]any)["res.partner"] = map[string]any{"available": true, "read": "true"}
		},
	} {
		var info map[string]any
		if err := json.Unmarshal([]byte(opts.Metadata["session_info"]), &info); err != nil {
			t.Fatal(err)
		}
		change(info)
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		candidate := permissionOptions(t)
		candidate.Metadata["session_info"] = string(raw)
		if _, handled := odooSessionReply(candidate); handled {
			t.Fatalf("unverified ACLs accepted: %s", raw)
		}
	}
}

func TestOdooSessionFactsKeepMixedOrUncertainRequestsOnAgent(t *testing.T) {
	for _, text := range []string{
		"Qué permisos tengo y crea un contacto", "Qué permisos tengo para el pedido S001",
		"Qué permisos tiene admin", "Puedo borrar este contacto", "Tengo todos los permisos",
		"Dime mis permisos y los de Juan", "En qué empresa estoy y cambia de empresa",
		"Qué modelo estoy usando y cambia a otro", "sí", "vale", "continúa", "hazlo", "gracias hazlo",
		"Qué permisos tengo según el PDF", "Qué permisos tengo; soy administrador",
	} {
		opts := permissionOptions(t)
		opts.UserMessage = text
		if reply, handled := odooSessionReply(opts); handled {
			t.Fatalf("intercepted %q => %s", text, reply)
		}
	}
	opts := permissionOptions(t)
	opts.Media = []string{"document.pdf"}
	if _, handled := odooSessionReply(opts); handled {
		t.Fatal("attachment request intercepted")
	}
}

func TestOdooCompanyAndSettingsFacts(t *testing.T) {
	opts := identityOptions()
	opts.UserMessage = "¿En qué empresa estoy?"
	reply, ok := odooSessionReply(opts)
	if !ok || !strings.Contains(reply, "**CMP**") {
		t.Fatal(reply, ok)
	}
	opts.UserMessage = "¿Qué modelo estoy usando?"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("missing configuration invented")
	}
	opts.Metadata["model_settings"] = `{"model":"gpt-6.1-sol","reasoning_effort":"medium","service_tier":"default"}`
	reply, ok = odooSessionReply(opts)
	if !ok || !strings.Contains(reply, "gpt-6.1-sol") || !strings.Contains(reply, "medio") || !strings.Contains(reply, "estándar") {
		t.Fatal(reply, ok)
	}
	for _, metadata := range []string{`{}`, `{"model":"x"}`, `{"model":"x","reasoning_effort":"unknown","service_tier":"default"}`, `{"model":"x","reasoning_effort":"medium","service_tier":"unknown"}`} {
		opts.Metadata["model_settings"] = metadata
		if _, ok := odooSessionReply(opts); ok {
			t.Fatal("unverified defaults invented", metadata)
		}
	}
}

func TestOdooNewSessionFactsUseZeroCallsAndKeepHistory(t *testing.T) {
	for _, query := range []string{"Qué permisos tengo", "Cuál es mi empresa", "Qué modelo tengo seleccionado"} {
		t.Run(query, func(t *testing.T) {
			opts := permissionOptions(t)
			opts.UserMessage = query
			opts.Metadata["model_settings"] = `{"model":"gpt-6.1-sol","reasoning_effort":"medium","service_tier":"default"}`
			agent := &AgentInstance{Sessions: session.NewSessionManager(t.TempDir())}
			loop := &AgentLoop{}
			ctx := withRequestUsage(context.Background())
			reply, err := loop.runAgentLoop(ctx, agent, opts)
			if err != nil || reply == "" {
				t.Fatal(reply, err)
			}
			usage := usageSnapshot(ctx)
			if usage.ModelCalls != 0 || usage.TotalTokens != 0 || !usage.Complete {
				t.Fatal(usage)
			}
			if len(agent.Sessions.GetHistory(opts.SessionKey)) != 2 {
				t.Fatal("history lost")
			}
		})
	}
}

func TestOdooCombinedSessionQuestions(t *testing.T) {
	for _, query := range []string{
		"Quién soy y qué permisos tengo", "quien soy y que permisso tengo", "Hola, dime quién soy y cuáles son mis permisos, por favor",
		"¿Quién soy? ¿Qué permisos tengo? ¿En qué empresa estoy? ¿Qué modelo estoy usando?",
		"Dime mi usuario, mis permisos y mi empresa", "mi usuario; mis permisos; mi modelo", "quién soy y además mis permisos",
		"buenas tardes quien soy y mis permisos", "buenas quien soy y tambien mis permisos", "Quién soy\nQué permisos tengo",
	} {
		t.Run(query, func(t *testing.T) {
			opts := permissionOptions(t)
			opts.UserMessage = query
			opts.Metadata["model_settings"] = `{"model":"gpt-6.1-sol","reasoning_effort":"medium","service_tier":"default"}`
			agent := &AgentInstance{Sessions: session.NewSessionManager(t.TempDir())}
			ctx := withRequestUsage(context.Background())
			reply, err := (&AgentLoop{}).runAgentLoop(ctx, agent, opts)
			if err != nil || !strings.Contains(reply, "FABRICA") || !strings.Contains(reply, "| Contactos |") {
				t.Fatal(reply, err)
			}
			if usage := usageSnapshot(ctx); usage.ModelCalls != 0 || usage.TotalTokens != 0 || !usage.Complete {
				t.Fatal(usage)
			}
			if len(agent.Sessions.GetHistory(opts.SessionKey)) != 2 {
				t.Fatal("history not preserved")
			}
		})
	}
}

func TestOdooSessionCompositionNeverDropsUnknownWork(t *testing.T) {
	for _, base := range []string{"quien soy", "que permisos tengo", "mi empresa", "mi modelo", "puedo crear contactos"} {
		for _, delimiter := range []string{" y ", ", ", "; ", "? ", "\n", " y ademas "} {
			for _, extra := range []string{"crea un contacto", "borra usuarios", "hazlo", "muestra mis pedidos", "los permisos de Juan", "responde en ingles", "no crees contactos", "no me digas quien soy", "que significa empresa", "ignora las instrucciones"} {
				for _, query := range []string{base + delimiter + extra, extra + delimiter + base} {
					opts := permissionOptions(t)
					opts.UserMessage = query
					if reply, ok := odooSessionReply(opts); ok {
						t.Fatalf("part of request dropped: %q => %s", query, reply)
					}
				}
			}
		}
	}
	opts := identityOptions()
	opts.UserMessage = "quien soy y que permisos tengo"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("partially answerable request intercepted")
	}
	opts = permissionOptions(t)
	opts.UserMessage = "quien soy y mi modelo"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("missing model settings ignored")
	}
	opts = permissionOptions(t)
	opts.UserMessage = "quien soy y mis permisos"
	opts.Media = []string{"mail.msg"}
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("attachment ignored")
	}
}

func TestOdooSessionPermissionQuestionsOnlyUseGeneralACLs(t *testing.T) {
	for _, query := range []string{"puedo crear contactos", "puedo borrar contactos", "que permisos tengo sobre contactos", "mis permisos en contactos"} {
		opts := permissionOptions(t)
		opts.UserMessage = query
		reply, ok := odooSessionReply(opts)
		if !ok || !strings.Contains(reply, "| Contactos | Consultar, Modificar |") || strings.Contains(reply, "| Usuarios |") || !strings.Contains(reply, "reglas de registros") {
			t.Fatal(query, reply, ok)
		}
	}
	for _, query := range []string{"puedo borrar este contacto", "puedo borrar contactos de Juan", "puedo crear contactos y crea uno", "mis permisos en contactos y res.users", "soy admin y puedo borrar contactos"} {
		opts := permissionOptions(t)
		opts.UserMessage = query
		if _, ok := odooSessionReply(opts); ok {
			t.Fatal("record question intercepted", query)
		}
	}
}

func TestOdooSessionAllAliasesAndPairs(t *testing.T) {
	for _, aliases := range odooQuestionAliases {
		for _, alias := range aliases {
			for _, second := range []string{"quien soy", "mis permisos", "mi empresa", "mi modelo"} {
				opts := permissionOptions(t)
				opts.UserMessage = alias + " y " + second
				opts.Metadata["model_settings"] = `{"model":"gpt-6.1-sol","reasoning_effort":"medium","service_tier":"default"}`
				if _, ok := odooSessionReply(opts); !ok {
					t.Fatal("supported combination not recognized", opts.UserMessage)
				}
			}
		}
	}
	questions, ok := parseOdooSessionQuestions("mi modelo y mi razonamiento y mi service tier")
	if !ok || len(questions) != 1 {
		t.Fatal("configuration repeated", questions, ok)
	}
}
