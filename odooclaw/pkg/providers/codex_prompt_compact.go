package providers

import (
	"bytes"
	"encoding/json"
)

// Compact only JSON layout. All keys, array entries and string content survive.
// Non-JSON document text is returned byte-for-byte. Stored history is untouched.
func compactToolJSON(content string) string {
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(content)) != nil {
		return content
	}
	return compact.String()
}
