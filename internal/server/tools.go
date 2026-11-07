package server

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tools.json is the source of truth for tool names, descriptions and argument schemas (ADR 0005).
// It is a copy of tests/snapshots/tools.json with the four pickup descriptions of ADR 0006.
//
//go:embed tools.json
var toolsJSON []byte

// toolDef is one tool as tools.json describes it, plus the arguments the decoder reads.
type toolDef struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	InputSchema json.RawMessage      `json:"inputSchema"`
	Annotations *mcp.ToolAnnotations `json:"annotations"`
	params      []param
}

func (d toolDef) tool() *mcp.Tool {
	return &mcp.Tool{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema, Annotations: d.Annotations}
}

// loadTools reads tools.json. It panics on a file it cannot use: that is a bug found by every test.
func loadTools() map[string]toolDef {
	var list []toolDef
	if err := json.Unmarshal(toolsJSON, &list); err != nil {
		panic(fmt.Errorf("tools.json: %w", err))
	}
	defs := make(map[string]toolDef, len(list))
	for _, d := range list {
		params, err := paramsOf(d.InputSchema)
		if err != nil {
			panic(fmt.Errorf("tools.json: %s: %w", d.Name, err))
		}
		d.params = params
		defs[d.Name] = d
	}
	return defs
}

// paramsOf turns an input schema into the arguments, in schema order.
func paramsOf(schema json.RawMessage) ([]param, error) {
	doc, err := parseDoc(schema)
	root, ok := doc.(*object)
	if err != nil || !ok {
		return nil, fmt.Errorf("bad schema")
	}
	props, _ := root.vals["properties"].(*object)
	if props == nil {
		return nil, nil
	}
	required := map[string]bool{}
	if list, ok := root.vals["required"].([]any); ok {
		for _, r := range list {
			s, _ := r.(string)
			required[s] = true
		}
	}
	var params []param
	for _, name := range props.keys {
		node, _ := props.vals[name].(*object)
		if node == nil {
			return nil, fmt.Errorf("argument %s", name)
		}
		p := param{name: name, required: required[name]}
		if variants := asList(node.vals["anyOf"]); variants != nil { // [type, {"type": "null"}]
			for _, v := range variants {
				variant, _ := v.(*object)
				if variant != nil && variant.vals["type"] == "null" {
					p.nullable = true
				} else {
					node = mergeNode(node, variant)
				}
			}
		}
		if err := p.fill(node); err != nil {
			return nil, fmt.Errorf("argument %s: %w", name, err)
		}
		params = append(params, p)
	}
	return params, nil
}

func mergeNode(node, variant *object) *object {
	out := &object{vals: map[string]any{}}
	for _, k := range node.keys {
		out.set(k, node.vals[k])
	}
	if variant != nil {
		for _, k := range variant.keys {
			out.set(k, variant.vals[k])
		}
	}
	return out
}

func (p *param) fill(node *object) error {
	typ, _ := node.vals["type"].(string)
	switch typ {
	case "string":
		p.kind = kString
		for _, e := range asList(node.vals["enum"]) {
			s, _ := e.(string)
			p.enum = append(p.enum, s)
		}
	case "integer":
		p.kind = kInt
	case "array":
		if elem, _ := node.vals["items"].(*object); elem == nil || elem.vals["type"] != "string" {
			return fmt.Errorf("only lists of strings")
		}
		p.kind = kList
	case "object":
		elem, _ := node.vals["additionalProperties"].(*object)
		switch {
		case elem != nil && elem.vals["type"] == "integer":
			p.kind = kIntMap
		case elem != nil && elem.vals["type"] == "string":
			p.kind = kStrMap
		default:
			return fmt.Errorf("only objects of integers or strings")
		}
	default:
		return fmt.Errorf("unknown type")
	}
	def, hasDefault := node.vals["default"]
	if hasDefault && def != nil {
		v, err := p.convert(def)
		if err != nil {
			return err
		}
		p.def = v
	}
	if hasDefault && def == nil && !p.nullable {
		return fmt.Errorf("a null default needs null in the schema")
	}
	return nil
}

func asList(v any) []any { l, _ := v.([]any); return l }
