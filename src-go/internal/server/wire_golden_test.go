package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/McAndersDK/handle-mcp-go/src-go/internal/goldentest"
)

type wireCall struct {
	Name         string          `json:"name"`
	Arguments    json.RawMessage `json:"arguments"`
	IsError      bool            `json:"is_error"`
	Text         string          `json:"text"`
	TextNoPrefix string          `json:"text_without_prefix"`
	TextCompared bool            `json:"text_compared"`
	Response     struct {
		ID int `json:"id"`
	} `json:"response"`
}

type wireSession struct {
	Calls      []wireCall `json:"calls"`
	Errors     []wireCall `json:"errors"`
	Initialize struct {
		Response struct {
			Result struct {
				Instructions string `json:"instructions"`
			} `json:"result"`
		} `json:"response"`
	} `json:"initialize"`
}

// loc is the argument a Python validation error names: its second line.
func loc(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return ""
	}
	return lines[1]
}

func metaIsOnlyServerInfo(t *testing.T, name string, meta map[string]any) {
	t.Helper()
	for k := range meta {
		if k != mcp.MetaKeyServerInfo {
			t.Errorf("%s: result._meta has %q, want only %q", name, k, mcp.MetaKeyServerInfo)
		}
	}
}

// TestWireGolden plays every call of session.json, in the order Python saw them, through the Go
// server and fake stores built like Python's. Answers are compared as parsed JSON.
// Named exceptions, counted and asserted at the end:
//   - argument errors: Python's text is pydantic's, ours is Danish (text_compared false): the
//     argument it names must be the same
//   - pickup: delivery_mode and pickup_store (goldentest.DropAdded)
//   - an unknown tool: a JSON-RPC error here, an isError result in Python
func TestWireGolden(t *testing.T) {
	var g wireSession
	goldentest.Read(t, "wire/session.json", &g)
	order := append(append([]wireCall{}, g.Calls...), g.Errors...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Response.ID < order[j].Response.ID })
	if len(g.Calls) != 14 || len(g.Errors) != 37 {
		t.Fatalf("session.json has %d calls and %d errors", len(g.Calls), len(g.Errors))
	}

	factories, _, _ := standard()
	s := connect(t, factories)
	if got := s.cs.InitializeResult().Instructions; got != g.Initialize.Response.Result.Instructions {
		t.Errorf("instructions %q, golden %q", got, g.Initialize.Response.Result.Instructions)
	}
	if err := s.cs.Ping(context.Background(), nil); err != nil {
		t.Errorf("ping: %v", err)
	}

	var compared, argumentErrors, pickup, unknownTool, withMeta int
	for _, c := range order {
		label := fmt.Sprintf("#%d %s %s", c.Response.ID, c.Name, c.Arguments)
		res := s.call(c.Name, c.Arguments)
		if c.Name == "no_such_tool" {
			unknownTool++
			if res.Err == nil || !strings.Contains(res.Err.Error(), "no_such_tool") {
				t.Errorf("%s: want a protocol error naming the tool, got %+v", label, res)
			}
			continue
		}
		if res.Err != nil {
			t.Errorf("%s: %v", label, res.Err)
			continue
		}
		metaIsOnlyServerInfo(t, label, res.Meta)
		if len(res.Meta) > 0 {
			withMeta++
		}
		if res.IsError != c.IsError {
			t.Errorf("%s: isError %v, golden %v: %s", label, res.IsError, c.IsError, res.Text)
			continue
		}
		switch {
		case !c.TextCompared:
			argumentErrors++
			if want := "Argumentet '" + loc(c.Text) + "'"; !strings.HasPrefix(res.Text, want) {
				t.Errorf("%s: error %q should start with %q", label, res.Text, want)
			}
		case c.IsError:
			compared++
			if res.Text != c.TextNoPrefix {
				t.Errorf("%s: error %q, golden %q", label, res.Text, c.TextNoPrefix)
			}
		default:
			compared++
			golden := decode(t, c.Text)
			actual := decode(t, res.Text)
			if !reflect.DeepEqual(goldentest.DropAdded(actual), golden) {
				t.Errorf("%s: answer differs from the golden\ngot:  %s\nwant: %s", label, res.Text, c.Text)
			}
			if !reflect.DeepEqual(decode(t, res.Text), golden) { // before dropping: the pickup keys are extra
				pickup++
			}
		}
	}
	t.Logf("%d results carry _meta with the serverInfo only; %d compared (%d of them differ only by the pickup keys), %d argument errors, %d unknown tool", withMeta, compared, pickup, argumentErrors, unknownTool)
	if argumentErrors != 4 || unknownTool != 1 || pickup != 12 {
		t.Errorf("named exceptions: %d argument errors (want 4), %d unknown tool (want 1), %d pickup (want 12)", argumentErrors, unknownTool, pickup)
	}
}

// The 12 compare_basket cases of compare/verdict.json that are a ToolError (an empty or too long
// list) were held back from package compare: here they run through the server.
func TestCompareBasketToolErrorsFromTheGolden(t *testing.T) {
	var g struct {
		Cases []struct {
			Name      string   `json:"name"`
			Items     []string `json:"items"`
			ToolError string   `json:"tool_error"`
		} `json:"cases"`
	}
	goldentest.Read(t, "compare/verdict.json", &g)
	factories, _, _ := standard()
	s := connect(t, factories)
	n := 0
	for _, c := range g.Cases {
		if c.ToolError == "" {
			continue
		}
		n++
		res := s.call("compare_basket", map[string]any{"items": c.Items})
		if want := strings.TrimPrefix(c.ToolError, "ToolError: "); !res.IsError || res.Text != want {
			t.Errorf("%s: isError %v text %q, want %q", c.Name, res.IsError, res.Text, want)
		}
	}
	if n != 12 {
		t.Errorf("%d ToolError cases, want 12", n)
	}
}
