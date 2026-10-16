package session

import "testing"

func TestDeleteOnlySelectedSessionAndPersistentFile(t *testing.T) {
	dir := t.TempDir()
	sm := NewSessionManager(dir)
	for _, key := range []string{"agent:main:odoo:direct:16:conversation:70", "agent:main:odoo:direct:24:conversation:71"} {
		sm.AddMessage(key, "user", "private message")
		if err := sm.Save(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := sm.Delete("agent:main:odoo:direct:16:conversation:70"); err != nil {
		t.Fatal(err)
	}
	if len(sm.GetHistory("agent:main:odoo:direct:16:conversation:70")) != 0 {
		t.Fatal("memory retained")
	}
	reloaded := NewSessionManager(dir)
	if len(reloaded.GetHistory("agent:main:odoo:direct:16:conversation:70")) != 0 {
		t.Fatal("disk retained")
	}
	if len(reloaded.GetHistory("agent:main:odoo:direct:24:conversation:71")) != 1 {
		t.Fatal("other user's history deleted")
	}
	if err := sm.Delete("../../outside"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
