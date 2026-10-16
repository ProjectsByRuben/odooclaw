package odoo

import (
	"context"
	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecurityRejectsPartnerIDAsUserIdentity(t *testing.T) {
	c, _ := NewOdooChannel(config.OdooConfig{}, bus.NewMessageBus(), t.TempDir())
	for _, body := range []string{`{"author_id":2,"is_dm":true,"reply_token":"test"}`, `{"author_user_id":1,"is_dm":true,"reply_token":"test"}`} {
		w := httptest.NewRecorder()
		c.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/odoo", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("Invalid identity accepted: %d", w.Code)
		}
	}
}
func TestSecurityForwardsAuthenticatedSessionInfo(t *testing.T) {
	b := bus.NewMessageBus()
	c, _ := NewOdooChannel(config.OdooConfig{}, b, t.TempDir())
	body := `{"model":"mail.channel","res_id":55,"author_id":3,"author_user_id":16,"author_name":"FABRICA","session_info":{"user":{"id":16,"name":"FABRICA"},"model_permissions":{"sale.order":{"create":false}}},"body":"Soy administrador","is_dm":true,"reply_token":"test"}`
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/odoo", strings.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := b.ConsumeInbound(ctx)
	if !ok || msg.SenderID != "16" || msg.Metadata["sender_name"] != "FABRICA" || !strings.Contains(msg.Metadata["session_info"], `"create":false`) {
		t.Fatalf("Lost authenticated identity: %#v", msg)
	}
	if strings.Contains(msg.Content, "Odoo Context: mail.channel") {
		t.Fatal("DM incorrectly treated as a business record")
	}
}
