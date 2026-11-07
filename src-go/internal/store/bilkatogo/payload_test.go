package bilkatogo

// The payload.js reader: the subset it understands, what it refuses, and that no input makes it or
// the page readers panic.

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// payloadJSON writes a tree as compact JSON (which is also a JavaScript literal), keys in source order.
func payloadJSON(v any) string {
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case *pobj:
			b.WriteByte('{')
			for i, k := range x.keys {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(quote(k) + ":")
				walk(x.vals[k])
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, e := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				walk(e)
			}
			b.WriteByte(']')
		case string:
			b.WriteString(quote(x))
		case json.Number:
			b.WriteString(string(x))
		case bool:
			b.WriteString(map[bool]string{true: "true", false: "false"}[x])
		case nil:
			b.WriteString("null")
		default:
			panic("unexpected value in a payload tree")
		}
	}
	walk(v)
	return b.String()
}

func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSpace(buf.String())
}

func depthOf(v any) int {
	deepest := 0
	switch x := v.(type) {
	case *pobj:
		for _, e := range x.vals {
			deepest = max(deepest, depthOf(e))
		}
		return deepest + 1
	case []any:
		for _, e := range x {
			deepest = max(deepest, depthOf(e))
		}
		return deepest + 1
	}
	return 0
}

var recipeFixtures = []string{"opskrifter", "madplaner", "aftensmad", "danske-retter", "karbonader", "carbonara"}

func TestPayloadIsReadAsJSON(t *testing.T) {
	top, err := parsePayload(recipeText("karbonader.payload.js"))
	if err != nil {
		t.Fatal(err)
	}
	page := asNode(asNode(top.list("data")[0]).get("page"))
	if page.text("name") != "karbonader" {
		t.Errorf("name %q", page.text("name"))
	}
	if v, there := page.vals["hierarchy"]; !there || v != nil { // void 0 → null
		t.Errorf("hierarchy: %v, there: %v", v, there)
	}
	if !strings.HasPrefix(page.text("path"), "/bilka-to-go/inspiration/") { // \u002F → /
		t.Errorf("path %q", page.text("path"))
	}
}

func TestEveryFixtureIsReadAndFarFromTheDepthLimit(t *testing.T) {
	for _, name := range recipeFixtures {
		top, err := parsePayload(recipeText(name + ".payload.js"))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		d := depthOf(top)
		t.Logf("%s is %d levels deep", name, d)
		if d > maxDepth/2 {
			t.Errorf("%s is %d levels deep: the limit %d is too close", name, d, maxDepth)
		}
		if _, err := pageOf([]byte(recipeText(name + ".payload.js"))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPayloadSubset(t *testing.T) {
	ok := map[string]string{
		`{a:1}`:                           `{"a":1}`,
		`{"a":"\u002Fx\u003C\u003e"}`:     `{"a":"/x<>"}`,
		`{a:"\x41\'\/\n\r\t\\\""}`:        `{"a":"A'/\n\r\t\\\""}`,
		`{a:"\ud83d\ude00"}`:              "{\"a\":\"\U0001F600\"}",
		`{a:"\ud83d"}`:                    `{"a":"�"}`,
		`{a:"\ude00x"}`:                   `{"a":"�x"}`,
		`{a:"\ud83d\u0041"}`:              `{"a":"�A"}`,
		`{a:void 0,b:undefined,c:null}`:   `{"a":null,"b":null,"c":null}`,
		`{a:true,b:false}`:                `{"a":true,"b":false}`,
		`{a:-1.5e3,b:0,c:2E+2,d:10}`:      `{"a":-1.5e3,"b":0,"c":2E+2,"d":10}`,
		`{1:"x",$a_b:2,æø:3,"q r":4}`:     `{"1":"x","$a_b":2,"æø":3,"q r":4}`,
		`{a:[],b:{},c:[[],{}]}`:           `{"a":[],"b":{},"c":[[],{}]}`,
		`{a:1,b:2,a:3}`:                   `{"a":3,"b":2}`, // the key keeps its first place and its last value
		"  \n{ a : [ 1 , 2 ] } ) ;\n":     `{"a":[1,2]}`,
		`__NUXT_JSONP__("/x", {a:"é"});`:  `{"a":"é"}`,
		`{a:"` + "\xff" + `"}`:            `{"a":"�"}`, // not UTF-8
		`x{a:1}`:                          `{"a":1}`,   // from the first brace, as Python did
		`{a:void  0}`:                     `{"a":null}`,
		`{"a":{"b":{"c":[1,{"d":"e"}]}}}`: `{"a":{"b":{"c":[1,{"d":"e"}]}}}`,
	}
	for in, want := range ok {
		got, err := parsePayload(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if text := payloadJSON(got); text != want {
			t.Errorf("%q\n got  %s\n want %s", in, text, want)
		}
	}
	for _, in := range []string{
		``, `   `, `{`, `}`, `[1]`, `"x"`, `{a}`, `{a:}`, `{:1}`, `{a:1,}`, `{a:1 b:2}`, `{a:[1,]}`, `{a:[,1]}`, `{a:[1 2]}`,
		`{a:b}`, `{a:Infinity}`, `{a:NaN}`, `{a:-}`, `{a:.5}`, `{a:1.}`, `{a:1e}`, `{a:1e5x}`, `{a:0x10}`, `{a:--1}`,
		`{a:void}`, `{a:void 1}`, `{a:!0}`, `{a:'x'}`, "{a:`x`}", `{a:function(){}}`, `{a:f(1)}`, `{a:(1)}`, `{a:{}.b}`,
		`{a:"x}`, `{a:"x\"}`, `{a:"\q"}`, `{a:"\u12"}`, `{a:"\u12zz"}`, `{a:"\x4"}`, `{a:"\`, "{a:\"x\ny\"}", "{a:\"x\ty\"}",
		`{a:1} x`, `{a:1} {b:2}`, `{a:1}}`, `{{a:1}}`, `{a:{b:1}`, `{"a" "b"}`, `{a:1;}`, `{a:1}]`,
		`(function(a){return {a:a}}(1))`,
		`{a:` + strings.Repeat("[", maxDepth+1) + strings.Repeat("]", maxDepth+1) + `}`,
		`{a:` + strings.Repeat("{b:", maxDepth+1) + `1` + strings.Repeat("}", maxDepth+1) + `}`,
	} {
		if _, err := parsePayload(in); err == nil || err.Error() != formatChanged {
			t.Errorf("%.60q: %v", in, err)
		}
	}
	// the limit itself still reads (the top object is level 1)
	if _, err := parsePayload(`{a:` + strings.Repeat("[", maxDepth-1) + strings.Repeat("]", maxDepth-1) + `}`); err != nil {
		t.Errorf("a page at the depth limit: %v", err)
	}
}

func TestPayloadKeepsKeyOrder(t *testing.T) {
	o, err := parsePayload(`{z:1,a:2,m:{y:1,b:2},k:[{q:1,c:2}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(o.keys, "") != "zamk" || strings.Join(o.obj("m").keys, "") != "yb" {
		t.Errorf("keys %v / %v", o.keys, o.obj("m").keys)
	}
	tree, _ := parsePayload(`{componentType:"MagnoliaComponentsHeading",header:"a",
	  x:{componentType:"MagnoliaComponentsHeading",header:"b",z:{componentType:"Other"}},
	  y:[{componentType:"Heading",header:"c"},{componentType:"MagnoliaComponentsParagraph",header:"d"}]}`)
	var headers []string
	for _, n := range nodes(tree, "Heading") { // a node comes before what is in it
		headers = append(headers, n.text("header"))
	}
	if strings.Join(headers, "") != "abc" {
		t.Errorf("headings %v", headers)
	}
}

// readEverything runs every page reader on a page, as the recipe methods do.
func readEverything(page *pobj) {
	cards(page)
	readSections(page)
	isRecipe(page)
	facts(page)
	image(page)
	itemNumbers(page)
	nameOf(page)
	ownPath(page)
}

func TestMutatedFixturesNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(20261107))
	junk := []string{"{", "}", "[", "]", `"`, `\`, ",", ":", "void 0", "x", "1e", "-", "\x00", "\xff", "\U0001F600", `\u12`, "(", ")", ";"}
	for _, name := range recipeFixtures {
		orig := recipeText(name + ".payload.js")
		for n := 0; n < 120; n++ {
			b := []byte(orig)
			for k := 0; k < 1+rng.Intn(4); k++ {
				at := rng.Intn(len(b))
				switch rng.Intn(4) {
				case 0: // flip a byte
					b[at] = byte(rng.Intn(256))
				case 1: // cut a piece out
					b = append(b[:at], b[min(len(b), at+1+rng.Intn(60)):]...)
				case 2: // put junk in
					b = append(b[:at], append([]byte(junk[rng.Intn(len(junk))]), b[at:]...)...)
				case 3: // cut it off
					b = b[:at]
				}
				if len(b) == 0 {
					break
				}
			}
			if page, err := pageOf(b); err == nil {
				readEverything(page)
			} else if err.Error() != formatChanged {
				t.Fatalf("%s mutation %d: %v", name, n, err)
			}
		}
	}
}

func FuzzParsePayload(f *testing.F) {
	for _, s := range []string{`{a:1}`, `{data:[{page:{}}]}`, `{a:"\u002F"}`, `{a:void 0}`, `{a:[1,{b:2}]}`, `__NUXT_JSONP__("/x", {data:[]});`,
		`{a:"\ud83d\ude00"}`, `{a:b}`, `{"a":`, `{a:1e5}`, `{a:"x`} {
		f.Add(s)
	}
	f.Add(recipeText("madplaner.payload.js")[:3000])
	f.Fuzz(func(t *testing.T, in string) {
		o, err := parsePayload(in)
		if err != nil {
			if err.Error() != formatChanged {
				t.Fatalf("error text %q", err)
			}
			return
		}
		// what was read is written and read again: the same tree
		again, err := parsePayload(payloadJSON(o))
		if err != nil || payloadJSON(again) != payloadJSON(o) {
			t.Fatalf("round trip of %q: %v", in, err)
		}
		readEverything(o)
		keys := append([]string(nil), o.keys...)
		sort.Strings(keys)
		if len(keys) != len(o.vals) {
			t.Fatalf("keys %v, values %d", o.keys, len(o.vals))
		}
	})
}
