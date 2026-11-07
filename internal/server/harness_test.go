package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/internal/store"
)

// session is a real MCP client talking to the server over an in-memory pipe.
type session struct {
	t  testing.TB
	cs *mcp.ClientSession
}

func standard() (map[string]Factory, *fakeStore, *fakeStore) {
	fake, broken := newFake("fake"), newBroken()
	return map[string]Factory{
		"fake":   func() store.Store { return fake },
		"broken": func() store.Store { return broken },
	}, fake, broken
}

func connect(t testing.TB, factories map[string]Factory) *session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := New(factories, "test").MCP().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "golden", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Wait() })
	return &session{t: t, cs: cs}
}

// result is what a tool call came back with.
type result struct {
	Text    string
	IsError bool
	Meta    map[string]any
	Err     error // a protocol error, not a tool error
}

// call sends the arguments as they are. A json.RawMessage goes out byte for byte.
func (s *session) call(name string, args any) result {
	s.t.Helper()
	if m, ok := args.(map[string]any); ok && m == nil {
		args = map[string]any{}
	}
	res, err := s.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return result{Err: err}
	}
	if len(res.Content) != 1 {
		s.t.Fatalf("%s: %d contents, want 1", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		s.t.Fatalf("%s: content is %T, want text", name, res.Content[0])
	}
	if res.StructuredContent != nil {
		s.t.Errorf("%s: structuredContent is set", name)
	}
	return result{Text: text.Text, IsError: res.IsError, Meta: res.Meta}
}

// decode parses the text as JSON with exact numbers.
func decode(t testing.TB, text string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, text)
	}
	return v
}

// jsonOf marshals a value to compact JSON for a call.
func jsonOf(t testing.TB, v any) json.RawMessage {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(b.Bytes())
}
