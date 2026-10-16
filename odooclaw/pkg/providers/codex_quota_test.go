package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexQuotaSanitizesAndKeepsBothWindows(t *testing.T) {
	q, err := parseQuota([]byte(`{"accountId":"private","rateLimits":{"limitId":"other","primary":{"usedPercent":99,"windowDurationMins":60,"resetsAt":123}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":80,"windowDurationMins":300,"resetsAt":1791972271},"secondary":{"usedPercent":23,"windowDurationMins":10080,"resetsAt":1792499001},"credits":{"balance":"private"}}}}`))
	if err != nil || !q.Available || len(q.Windows) != 2 || q.Windows[0].Remaining != 20 || q.Windows[1].Remaining != 77 {
		t.Fatalf("unexpected snapshot: %+v %v", q, err)
	}
	b, _ := json.Marshal(q)
	var data map[string]any
	json.Unmarshal(b, &data)
	if len(data) != 4 {
		t.Fatalf("unexpected public fields: %s", b)
	}
}
func TestCodexQuotaDoesNotInventMissingUsage(t *testing.T) {
	for _, data := range []string{`{}`, `{"rateLimits":{"primary":{"windowDurationMins":300,"resetsAt":123}}}`, `{"rateLimits":{"limitId":"other","primary":{"usedPercent":20,"windowDurationMins":300,"resetsAt":123}}}`, `{"rateLimits":{"primary":{"usedPercent":-5,"windowDurationMins":300,"resetsAt":123}}}`} {
		q, _ := parseQuota([]byte(data))
		if q.Available {
			t.Fatalf("fabricated quota: %s", data)
		}
	}
}
func TestCodexQuotaProtocolAndCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
read init
case "$init" in *initialize*) ;; *) exit 1 ;; esac
[ -z "$ODOOCLAW_QUOTA_TEST_SECRET" ] || exit 1
[ -z "$ODOO_QUOTA_TEST_SECRET" ] || exit 1
printf '%s\n' '{"id":1,"result":{}}'
read initialized
read query
case "$query" in *account/rateLimits/read*) ;; *) exit 1 ;; esac
printf '%s\n' '{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":100,"windowDurationMins":300,"resetsAt":1791972271}}}}'
read hold
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ODOOCLAW_QUOTA_TEST_SECRET", "secret")
	t.Setenv("ODOO_QUOTA_TEST_SECRET", "secret")
	p := &CodexCliProvider{command: path}
	read := p.NewQuotaReader()
	q := read(context.Background())
	if !q.Available || q.Windows[0].Remaining != 0 {
		t.Fatalf("protocol failed: %+v", q)
	}
	os.Remove(path)
	q2 := read(context.Background())
	if !q2.Available || q2.CheckedAt != q.CheckedAt {
		t.Fatal("cache not shared")
	}
}
func TestCodexQuotaTimeoutReturnsUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	os.WriteFile(path, []byte("#!/bin/sh\nread init\nsleep 30 &\nwait\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	q := (&CodexCliProvider{command: path}).NewQuotaReader()(ctx)
	if q.Available {
		t.Fatal("timeout invented quota")
	}
}
