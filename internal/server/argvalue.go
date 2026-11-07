package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// object is a decoded JSON object that keeps the order of its keys. A repeated key keeps its first
// place and takes the last value, as a Python dict does.
type object struct {
	keys []string
	vals map[string]any
}

func (o *object) set(key string, v any) {
	if _, seen := o.vals[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

const maxDepth = 64

var errDeep = errors.New("too deep")

// parseDoc decodes one JSON document. Values are string, json.Number, bool, nil, []any and *object.
// Anything after the document is an error.
func parseDoc(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := parseValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data")
	}
	return v, nil
}

func parseValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, errDeep
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if delim == '[' {
		list := []any{}
		for dec.More() {
			v, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err := dec.Token() // ]
		return list, err
	}
	obj := &object{vals: map[string]any{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := keyTok.(string)
		v, err := parseValue(dec, depth+1)
		if err != nil {
			return nil, err
		}
		obj.set(key, v)
	}
	_, err = dec.Token() // }
	return obj, err
}
