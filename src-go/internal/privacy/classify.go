package privacy

import (
	"reflect"
	"sort"
	"strings"
)

// Class says how an answer key is treated by Scrub.
type Class int

const (
	Unclassified Class = iota
	Passthrough        // shown as the store wrote it
	FreeText           // scrubbed with ScrubText
	Personal           // replaced by Redacted
)

// PassthroughKeys are the answer keys that carry no personal data and are not free text.
// A new field on a model fails the classification test until it is listed here (ADR 0005).
// The server phase adds its own answer keys (tool results that are not models).
var PassthroughKeys = setOf(
	"store", "id", "name", "price", "brand", "size", "unit_price", "unit", "ean",
	"on_offer", "offer_text", "before_price", "offer_until", "available", "category",
	"campaign", "max_per_order", "url", "time", "persons", "group", "quantity",
	"ingredient", "product", "have_at_home", "alternatives", "title", "steps",
	"min_persons", "max_persons", "description", "image", "ingredients",
	"ingredients_persons", "method", "tip", "items", "total", "product_id",
	"line_total", "lines", "products_total", "fees", "minimum_order", "minimum_met",
	"delivery", "requested", "applied", "ok", "home_delivery", "delivery_fee",
	"other_fees", "source", "chosen_start", "chosen_end", "edit_deadline",
	"next_available", "start", "end", "kind", "status", "placed_at",
	"delivery_start", "delivery_end",
)

// Classify returns how Scrub treats a key. Personal wins over free text, as in scrubItem.
func Classify(key string) Class {
	n := norm(key)
	switch {
	case personalKeys[n]:
		return Personal
	case freeTextKeys[n]:
		return FreeText
	case PassthroughKeys[key]:
		return Passthrough
	}
	return Unclassified
}

// JSONKeys lists every JSON key of the given struct types, following nested structs,
// slices, pointers and maps (map keys are data, not fields). Sorted and unique.
func JSONKeys(types ...reflect.Type) []string {
	seen := map[string]bool{}
	visited := map[reflect.Type]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || visited[t] {
			return
		}
		visited[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			seen[name] = true
			walk(f.Type)
		}
	}
	for _, t := range types {
		walk(t)
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// UnclassifiedKeys lists the keys of the types that are in none of the three classes.
func UnclassifiedKeys(types ...reflect.Type) []string {
	var out []string
	for _, k := range JSONKeys(types...) {
		if Classify(k) == Unclassified {
			out = append(out, k)
		}
	}
	return out
}

// PersonalKeysOf lists the keys of the types that Scrub would redact. A model must have none:
// a personal key on a model is a leak by design.
func PersonalKeysOf(types ...reflect.Type) []string {
	var out []string
	for _, k := range JSONKeys(types...) {
		if Classify(k) == Personal {
			out = append(out, k)
		}
	}
	return out
}
