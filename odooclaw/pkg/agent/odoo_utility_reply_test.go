package agent

import (
	"context"
	"github.com/nicolasramos/odooclaw/pkg/session"
	"strings"
	"testing"
	"time"
)

func TestOdooUtilitiesExactResults(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 55, 3, 0, time.UTC)
	cases := []struct{ q, want string }{
		{"¿Qué hora es en Colombia?", "07:55:03"},
		{"hola, ¿qué hora es ahora en Colombia?", "07:55:03"},
		{"Dime la hora actual en Bogotá por favor", "07:55:03"},
		{"que fecha es hoy en colombia", "21/10/2026"},
		{"que hora y fecha es en madrid", "14:55:03"},
		{"que dia es hoy", "miércoles"},
		{"que hora es en japon", "21:55:03"},
		{"que hora es en españa", "varias zonas"},
		{"¿Cuánto es 12,5 * 4?", "50"},
		{"100 / 4", "25"}, {"-10 - -3", "-7"},
		{"1 / 3", "≈ 0,33333333"}, {"5 / 0", "cero"},
		{"cuanto es el 21% de 100", "21"},
		{"convierte 2,5 kg a g", "2500 g"},
		{"120 minutos en horas", "2 horas"},
		{"2 m a mm", "2000 mm"},
		{"muchas gracias", "De nada"},
		{"como uso odooclaw", "permisos"},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			got, ok := odooUtilityReply(tc.q, "Europe/Madrid", now)
			if !ok || !strings.Contains(got, tc.want) {
				t.Fatalf("%q => %q, %v; expected %s", tc.q, got, ok, tc.want)
			}
		})
	}
	winter, _ := odooUtilityReply("hora en madrid", "", time.Date(2026, 1, 21, 12, 0, 0, 0, time.UTC))
	if !strings.Contains(winter, "13:00:00") {
		t.Fatal("DST ignored", winter)
	}
	midnight, _ := odooUtilityReply("fecha en colombia", "", time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC))
	if !strings.Contains(midnight, "20/10/2026") {
		t.Fatal("local date ignored", midnight)
	}
	noZone, ok := odooUtilityReply("que hora es", "", now)
	if !ok || !strings.Contains(noZone, "Indica") {
		t.Fatal("missing timezone invented", noZone)
	}
}

func TestOdooUtilitiesDoNotInterceptWorkOrContext(t *testing.T) {
	for _, q := range []string{
		"que hora es en colombia y crea un pedido", "hora en madrid; borra un contacto", "hora en colombia segun este pdf",
		"que hora es en colombia\n[Contexto: sale.order 1]", "cuanto es 2 + 3 y actualiza el precio", "2 kg a litros",
		"calcula el iva de este pedido", "gracias, ahora borra el contacto", "1e100 + 1", "2 + 3 * 4", "hola que hora es en colombia y muestra mis pedidos",
		"fecha de entrega de este pedido", "hora trabajada por juan", "hora en madrid en formato json",
	} {
		if reply, ok := odooUtilityReply(q, "Europe/Madrid", time.Now()); ok {
			t.Fatalf("intercepted %q => %s", q, reply)
		}
	}
	opts := identityOptions()
	opts.UserMessage = "hora en colombia"
	opts.Media = []string{"pdf"}
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("attachment ignored")
	}
	opts.Media = nil
	opts.SenderID = "2"
	if _, ok := odooSessionReply(opts); ok {
		t.Fatal("identity mismatch bypassed")
	}
}

func TestOdooUtilitiesZeroInferenceAndHistory(t *testing.T) {
	for _, q := range []string{"que hora es en colombia?", "2 + 3", "21% de 100", "2 kg a g", "gracias"} {
		t.Run(q, func(t *testing.T) {
			agent := &AgentInstance{Sessions: session.NewSessionManager(t.TempDir())}
			loop := &AgentLoop{}
			ctx := withRequestUsage(context.Background())
			opts := identityOptions()
			opts.UserMessage = q
			// Nil provider and tools deliberately prove there are no hidden model calls.
			reply, err := loop.runAgentLoop(ctx, agent, opts)
			if err != nil || reply == "" {
				t.Fatal(reply, err)
			}
			usage := usageSnapshot(ctx)
			if usage.ModelCalls != 0 || usage.TotalTokens != 0 || !usage.Complete {
				t.Fatal(usage)
			}
			history := agent.Sessions.GetHistory(opts.SessionKey)
			if len(history) != 2 || history[0].Content != q || history[1].Content != reply {
				t.Fatal(history)
			}
		})
	}
}
