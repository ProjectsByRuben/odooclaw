package odoo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/config"
)

func TestProgressHeartbeatKeepsLatestActivityAndCannotCrossRequests(t *testing.T) {
	var replies []OdooReplyPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reply OdooReplyPayload
		if err := json.NewDecoder(r.Body).Decode(&reply); err != nil {
			t.Error(err)
		}
		replies = append(replies, reply)
		w.WriteHeader(200)
	}))
	defer server.Close()
	t.Setenv("ODOO_URL", server.URL)
	t.Setenv("ODOO_DB", "")
	c, err := NewOdooChannel(config.OdooConfig{}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatID := "mail.channel_123"
	c.pendingTokens.Store(chatID, "first")
	ctx := context.Background()
	if err := c.Send(ctx, bus.OutboundMessage{ChatID: chatID, Content: "Analizando", Progress: true}); err != nil {
		t.Fatal(err)
	}
	// MarkRequestRunning updates desired activity before the dispatcher sends it.
	// Throttling must compare with the last transmitted phase, not desired state.
	c.progressActivity.Store(chatID, "Consultando datos")
	// Distinct phases must be sent immediately, even within two seconds.
	if err := c.Send(ctx, bus.OutboundMessage{ChatID: chatID, Content: "Consultando datos", Progress: true}); err != nil {
		t.Fatal(err)
	}
	c.progressLast.Store(chatID, time.Now().Add(-10*time.Second))
	if err := c.send(ctx, bus.OutboundMessage{ChatID: chatID, Progress: true}, "first"); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 3 || replies[2].Message != "Consultando datos" {
		t.Fatalf("lost phase: %+v", replies)
	}
	usage := &bus.TokenUsage{InputTokens: 300, CachedInputTokens: 120, OutputTokens: 60, TotalTokens: 360, ModelCalls: 3, Complete: true}
	if err := c.Send(ctx, bus.OutboundMessage{ChatID: chatID, Content: "Respuesta final", TokenUsage: usage}); err != nil {
		t.Fatal(err)
	}
	if replies[3].TokenUsage == nil || *replies[3].TokenUsage != *usage {
		t.Fatalf("final usage missing from HTTP payload: %+v", replies[3])
	}
	for _, reply := range replies[:3] {
		if reply.TokenUsage != nil {
			t.Fatal("progress should not include usage")
		}
	}
	if _, ok := c.progressActivity.Load(chatID); ok {
		t.Fatal("completed activity retained")
	}
	c.pendingTokens.Store(chatID, "second")
	c.progressActivity.Store(chatID, "Nueva solicitud")
	if err := c.send(ctx, bus.OutboundMessage{ChatID: chatID, Progress: true}, "first"); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 4 {
		t.Fatal("old heartbeat was sent under a new capability")
	}
}
