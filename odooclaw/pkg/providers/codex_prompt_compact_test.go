package providers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCompactToolJSONPreservesDocumentAndAllRecordValues(t *testing.T) {
	values := map[string]any{"name": "Pedido de prueba", "amount_total": 1234.56, "document": strings.Repeat("Texto PDF / correo\nCon espacios y símbolos: ñ & < >\n", 400), "ids": []int{1, 2, 3}, "active": false, "missing": nil}
	pretty, _ := json.MarshalIndent(values, "", "  ")
	compact := compactToolJSON(string(pretty))
	var a, b any
	json.Unmarshal(pretty, &a)
	json.Unmarshal([]byte(compact), &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("document or values changed")
	}
	if len(compact) >= len(pretty) {
		t.Fatal("layout not compacted")
	}
	text := "Correo sin JSON\n\n Asunto: NCR\n  preservar espacios\n"
	if compactToolJSON(text) != text {
		t.Fatal("plain text modified")
	}
	if compactToolJSON("{invalid JSON") != "{invalid JSON" {
		t.Fatal("invalid text altered")
	}
}
func TestCodexPromptCompactsWithoutModifyingHistory(t *testing.T) {
	original := "[\n {\n  \"id\": 5,\n  \"name\": \"Contacto\"\n }\n]"
	messages := []Message{{Role: "user", Content: "Leer PDF"}, {Role: "tool", ToolCallID: "read_pdf", Content: original}}
	prompt := (&CodexCliProvider{}).buildPrompt(messages, nil)
	if !strings.Contains(prompt, `[{"id":5,"name":"Contacto"}]`) || messages[1].Content != original {
		t.Fatal("history changed or compact output missing")
	}
}
