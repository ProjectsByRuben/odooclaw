package memory

import "testing"

func TestDeleteConversationRecipesCannotDeleteOtherUsers(t *testing.T) {
	s, err := NewRecipeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	for _, v := range []struct{ query, chat, uid string }{{"first", "mail.channel_70", "16"}, {"second", "mail.channel_71", "16"}, {"third", "mail.channel_70", "24"}} {
		if _, err := s.SaveRecipe(v.query, "tool", "{}", "odoo", v.chat, v.uid, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteConversation("odoo", "mail.channel_70", "16"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM recipes").Scan(&n); err != nil || n != 2 {
		t.Fatalf("other recipes affected: %d %v", n, err)
	}
}
