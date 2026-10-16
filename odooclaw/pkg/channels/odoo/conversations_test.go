package odoo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nicolasramos/odooclaw/pkg/bus"
	"github.com/nicolasramos/odooclaw/pkg/config"
)

func TestConversationContextIsBoundToUserAndChannel(t *testing.T) {
	cases := []struct {
		uid, channel, conversation int
		want                       string
	}{
		{16, 101, 101, "16:conversation:101"},
		{16, 102, 102, "16:conversation:102"},
		{24, 101, 101, "24:conversation:101"},
		{16, 55, 0, "16"},    // preserve legacy DM history
		{16, 101, 102, "16"}, // reject mismatched conversation metadata
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			b := bus.NewMessageBus()
			c, err := NewOdooChannel(config.OdooConfig{}, b, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"message_id":1,"model":"mail.channel","res_id":%d,"author_user_id":%d,"is_dm":true,"conversation_id":%d,"reply_token":"test","body":"hola"}`, tc.channel, tc.uid, tc.conversation)
			w := httptest.NewRecorder()
			c.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/odoo", strings.NewReader(body)))
			if w.Code != 200 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			msg, ok := b.ConsumeInbound(ctx)
			if !ok || msg.Peer.ID != tc.want || msg.SenderID != fmt.Sprint(tc.uid) {
				t.Fatalf("wrong identity/session: %+v", msg)
			}
		})
	}
}

func TestConversationDeleteAuthenticationBusyAndReplay(t *testing.T) {
	c, err := NewOdooChannel(config.OdooConfig{WebhookToken: "secret"}, bus.NewMessageBus(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.SetConversationDeleter(func(uid, channelID, conversationID int) error {
		if uid != 16 || channelID != 70 || conversationID != 70 {
			t.Fatalf("wrong context identity %d/%d", uid, conversationID)
		}
		calls++
		return nil
	})
	system := func(event, token string) int {
		r := httptest.NewRequest("POST", "/webhook/odoo/system", strings.NewReader(fmt.Sprintf(`{"event":%q,"user_id":16,"channel_id":70,"conversation_id":70}`, event)))
		r.Header.Set("X-OdooClaw-Token", token)
		w := httptest.NewRecorder()
		c.ServeHTTP(w, r)
		return w.Code
	}
	if system("conversation_delete", "wrong") != 401 || calls != 0 {
		t.Fatal("authentication failed")
	}
	c.pendingTokens.Store("mail.channel_70", "pending")
	if system("conversation_delete", "secret") != 409 || calls != 0 {
		t.Fatal("busy request deleted")
	}
	c.pendingTokens.Delete("mail.channel_70")
	if system("conversation_delete", "secret") != 200 || calls != 1 {
		t.Fatal("idle context not deleted")
	}
	r := httptest.NewRequest("POST", "/webhook/odoo", strings.NewReader(`{"model":"mail.channel","res_id":70,"author_user_id":16,"is_dm":true,"conversation_id":70,"reply_token":"replay"}`))
	r.Header.Set("X-OdooClaw-Token", "secret")
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != 410 {
		t.Fatal("deleted chat admitted a replay")
	}
	if system("conversation_restore", "secret") != 200 {
		t.Fatal("rollback restoration failed")
	}
	if _, retired := c.retiredConversations.Load("mail.channel_70"); retired {
		t.Fatal("retained chat permanently blocked")
	}
}
