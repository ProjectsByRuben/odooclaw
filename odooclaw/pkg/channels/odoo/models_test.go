package odoo

import (
	"context"
	"encoding/json"
	"github.com/nicolasramos/odooclaw/pkg/config"
	"github.com/nicolasramos/odooclaw/pkg/providers"
	"net/http"
	"testing"
)

func TestModelCatalogNeedsSharedSecret(t *testing.T) {
	c, _, _ := newSystemTestChannel(t, config.OdooConfig{}, t.TempDir())
	calls := 0
	c.SetModelReader(func(context.Context) providers.ModelCatalog {
		calls++
		return providers.ModelCatalog{Available: true, Models: []providers.ModelChoice{{ID: "model-a"}}}
	})
	for _, token := range []string{"", "wrong"} {
		w := doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_catalog"}`, token)
		if w.Code != http.StatusUnauthorized {
			t.Fatal("catalog available without shared secret")
		}
	}
	c.config.WebhookToken = "secret"
	w := doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_catalog"}`, "wrong")
	if w.Code != 401 || calls != 0 {
		t.Fatal("reader used without authorization")
	}
	w = doSystemRequest(t, c, http.MethodPost, "/webhook/odoo/system", `{"event":"model_catalog"}`, "secret")
	var out providers.ModelCatalog
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Available || calls != 1 {
		t.Fatal(w.Body.String())
	}
}
