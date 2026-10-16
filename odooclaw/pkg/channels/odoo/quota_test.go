package odoo

import (
	"context"
	"encoding/json"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"net/http"
	"testing"
)

func TestQuotaRequiresSharedSecretAndReturnsOnlyReaderSnapshot(t *testing.T) {
	c, _, _ := newSystemTestChannel(t, config.OdooConfig{}, t.TempDir())
	calls := 0
	c.SetQuotaReader(func(context.Context) providers.ModelQuota {
		calls++
		return providers.ModelQuota{Available: true, Scope: "shared_codex_account", Windows: []providers.QuotaWindow{{Remaining: 20, DurationMinutes: 300, ResetsAt: 123}}}
	})
	for _, token := range []string{"", "wrong"} {
		w := doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_usage"}`, token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unconfigured reader exposed quota: %d", w.Code)
		}
	}
	c.config.WebhookToken = "secret"
	w := doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_usage"}`, "wrong")
	if w.Code != 401 || calls != 0 {
		t.Fatal("reader called without authorization")
	}
	w = doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_usage"}`, "secret")
	var q providers.ModelQuota
	json.Unmarshal(w.Body.Bytes(), &q)
	if w.Code != 200 || !q.Available || calls != 1 {
		t.Fatalf("unexpected quota response: %s", w.Body.String())
	}
}
