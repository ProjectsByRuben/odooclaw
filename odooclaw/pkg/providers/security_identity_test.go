package providers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityCodexDoesNotReceiveOdooSecrets(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "codex-test")
	script := `#!/bin/sh
[ -z "${ODOO_PASSWORD-}" ] || exit 21
[ -z "${ODOOCLAW_REPLY_TOKEN-}" ] || exit 22
[ -n "${CODEX_HOME-}" ] || exit 23
case " $* " in *" --sandbox read-only "*) ;; *) exit 24;; esac
case " $* " in *" --dangerously-bypass-approvals-and-sandbox "*) exit 25;; esac
cat >/dev/null
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}'
`
	if err := os.WriteFile(command, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ODOO_PASSWORD", "test-secret")
	t.Setenv("ODOOCLAW_REPLY_TOKEN", "test-token")
	t.Setenv("CODEX_HOME", dir)
	provider := NewCodexCliProvider(dir)
	provider.command = command
	result, err := provider.Chat(context.Background(), []Message{{Role: "user", Content: "hello"}}, nil, "codex-cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" {
		t.Fatalf("Unexpected result %#v", result)
	}
}
