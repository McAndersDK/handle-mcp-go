// Package privacy strips personal data before anything leaves the server.
//
// Every tool answer goes through Scrub (see Out). Values are replaced with a marker
// instead of deleted, so the model can still tell that an order had a delivery address.
// Keys are matched without case, "_" or "-": doorCode, DoorCode and door_code are one key.
//
// Free text from a store (warnings, messages, errors) can echo personal data under a
// harmless key. ScrubText replaces emails, phone numbers and addresses in it. It runs on
// the free-text keys and on every error text, not on product names: "Kildevand 0,5 l 24
// stk" must pass as the store wrote it. False positives are accepted in free text: an
// 8-digit id reads as a phone number and "1000 Stk" as a postal code and city.
package privacy

import "strings"

// Redacted replaces every personal value.
const Redacted = "[fjernet af handle-mcp]"

// personalKeys are normalised keys (see norm) whose value is replaced.
var personalKeys = setOf(
	// whole blocks that are nothing but personal data
	"invoiceaddress", "deliveryaddress", "billingaddress", "homeaddress",
	// names (not "name": that is the product name)
	"customername", "contactperson", "recipientname", "fullname", "firstname",
	"lastname", "username", "membername",
	// contact
	"email", "mail", "emailaddress", "phone", "phonenumber", "mobile",
	"mobilenumber", "mobilephone", "telephone",
	// where we live and how to get in
	"address", "street", "streetname", "housenumber", "floor", "door", "zip",
	"zipcode", "postalcode", "postcode", "city", "lat", "lng", "latitude",
	"longitude", "doorcode", "unattendednotes", "deliverycomment", "deliverynote",
	"deliverynotes", "deliveryplacement", "pickupcode", "orderpickupcode",
	"dawauid", // Bilka: DAWA id that points to one Danish address
	// account ids
	"uid", "tid", "customerid", "memberid", "userid",
	// payment (never fetched, but never passed on either)
	"card", "cards", "cardnumber", "maskedcardnumber", "creditcard", "iban",
)

// freeTextKeys hold store text whose value may contain personal data.
// "notes" is not in Python's list; Go adds it on purpose (ADR 0005).
var freeTextKeys = setOf("warnings", "message", "messages", "note", "notes", "error")

func setOf(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// norm makes "Door_Code", "door-code" and "doorCode" the same key.
func norm(key string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
}

// Scrub returns a copy of decoded JSON (map[string]any, []any, json.Number, string,
// bool, nil) with all personal fields replaced. The input is not changed.
func Scrub(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = scrubItem(k, item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = Scrub(item)
		}
		return out
	}
	return value
}

func scrubItem(key string, value any) any {
	n := norm(key)
	if personalKeys[n] {
		return Redacted
	}
	if freeTextKeys[n] {
		return ScrubText(Scrub(value))
	}
	return Scrub(value)
}
