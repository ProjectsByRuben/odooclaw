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

func TestJSONCompactionPreservesAllBytesExceptLayoutAndHTMLRepresentation(t *testing.T) {
	source := `{ "n":9007199254740993123456, "decimal":1.2300e-25, "duplicate":"first", "duplicate":"second", "doc":"\u003cPDF\u003E \u0026 email ñ\n  keep spaces", "literal":"\\u003c", "nested":[null,false,{"x":"\u003c"}] }`
	got := compactToolJSON(source)
	expected := `{"n":9007199254740993123456,"decimal":1.2300e-25,"duplicate":"first","duplicate":"second","doc":"<PDF> & email ñ\n  keep spaces","literal":"\\u003c","nested":[null,false,{"x":"<"}]}`
	if got != expected {
		t.Fatalf("representation lost information:\n%s\n%s", got, expected)
	}
	var before, after any
	if json.Unmarshal([]byte(source), &before) != nil || json.Unmarshal([]byte(got), &after) != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("decoded values changed")
	}
	doc := strings.Repeat(`\u003cdiv\u003eA \u0026 B\u003c/div\u003e`, 500)
	compact := compactToolJSON(`{"document":"` + doc + `"}`)
	t.Logf("HTML JSON sample: %d -> %d bytes; same decoded document", len(doc)+15, len(compact))
}
