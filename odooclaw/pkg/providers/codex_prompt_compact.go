package providers

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Compact only JSON layout. All keys, array entries and string content survive.
// Non-JSON document text is returned byte-for-byte. Stored history is untouched.
func compactToolJSON(content string) string {
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(content)) != nil {
		return content
	}
	return unescapeJSONHTML(compact.String())
}

// Change only genuine JSON escapes inside strings. Do not decode/re-encode
// objects: preserve duplicate keys, number lexemes and every other byte.
func unescapeJSONHTML(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	inString := false
	for i := 0; i < len(value); {
		ch := value[i]
		if inString && ch == '\\' && i+1 < len(value) {
			if value[i+1] == 'u' && i+6 <= len(value) {
				var literal byte
				switch strings.ToLower(value[i+2 : i+6]) {
				case "003c":
					literal = '<'
				case "003e":
					literal = '>'
				case "0026":
					literal = '&'
				}
				if literal != 0 {
					out.WriteByte(literal)
					i += 6
					continue
				}
			}
			out.WriteString(value[i : i+2])
			i += 2
			continue
		}
		if ch == '"' {
			inString = !inString
		}
		out.WriteByte(ch)
		i++
	}
	return out.String()
}
