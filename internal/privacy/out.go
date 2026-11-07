package privacy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Out is the one exit of every tool answer: marshal, decode, scrub, marshal with
// 2-space indent. Numbers come through as the store wrote them. An answer that is not a
// JSON object becomes {"result": ...}, so the client always gets an object.
func Out(value any) (string, error) {
	raw, err := encode(value, "")
	if err != nil {
		return "", fmt.Errorf("answer: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var plain any
	if err := dec.Decode(&plain); err != nil {
		return "", fmt.Errorf("answer: %w", err)
	}
	scrubbed := Scrub(plain)
	if _, ok := scrubbed.(map[string]any); !ok {
		scrubbed = map[string]any{"result": scrubbed}
	}
	text, err := encode(scrubbed, "  ")
	if err != nil {
		return "", fmt.Errorf("answer: %w", err)
	}
	return text, nil
}

// encode marshals without HTML escaping ("&" and "<" stay as they are).
func encode(value any, indent string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
