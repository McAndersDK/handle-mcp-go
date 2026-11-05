package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	gigyaURL   = "https://" + gigyaHost
	bilkaAPI   = "https://" + bilkaHost + "/api"
	algoliaURL = "https://" + algoliaHost + "/1/indexes/*/queries"
	gigyaKey   = "3_tA6BbV434FQqN73HnUG1KA3qFv8KiG4OqLu9eWPh7sKRqRizH5Vfv5Larmgrb4I2"
	badLogin   = 403042
	rateLimit  = 403048
	bilkaLimit = 30 * time.Second
	jwtSeconds = "86400"
	algoliaApp = "F9VBJLR1BK"
	algoliaKey = "1deaf41c87e729779f7695c00f190cc9" // public search-only key
	algoliaIdx = "prod_BILKATOGO_PRODUCTS"
)

type bilkaRun struct {
	a   *app
	c   *http.Client
	jwt string // memory only
}

// gigya posts a form. Error codes are reported as a class only, never the message.
func (b *bilkaRun) gigya(step, method string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequest("POST", gigyaURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fail(step, "request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, data, err := b.a.do(b.c, step, req, modeGigya)
	if err != nil {
		return nil, err
	}
	m, err := obj(step, data)
	if err != nil {
		return nil, err
	}
	code := num(m["errorCode"])
	switch {
	case code == 0:
		return m, nil
	case code == badLogin:
		return nil, fail(step, "bad_login")
	case code == rateLimit || code >= 500000:
		return nil, fail(step, "rate_limit")
	}
	return nil, fail(step, "other_error")
}

func (b *bilkaRun) jwtFor(step, token string) (string, error) {
	m, err := b.gigya(step, "accounts.getJWT",
		url.Values{"apiKey": {gigyaKey}, "login_token": {token}, "expiration": {jwtSeconds}})
	if err != nil {
		return "", err
	}
	jwt := str(m["id_token"])
	if jwt == "" {
		return "", fail(step, "unexpected_shape")
	}
	return jwt, nil
}

// loginJWT never sends a `cart` cookie: the guard refuses the request if one is present.
func (b *bilkaRun) loginJWT(jwt string) error {
	req, err := http.NewRequest("POST", bilkaAPI+"/auth/LoginJWT?u=w", nil)
	if err != nil {
		return fail("loginjwt", "request")
	}
	req.Header.Set("jwt_token", jwt)
	status, _, err := b.a.do(b.c, "loginjwt", req, modeRaw)
	if err != nil {
		return err
	}
	if status != 200 {
		return fail("loginjwt", "not_200")
	}
	return nil
}

// cart returns the order line count (fee groups left out), home delivery and the store id.
func (b *bilkaRun) cart(step string) (lines int, home bool, sid string, err error) {
	req, err := http.NewRequest("GET", bilkaAPI+"/shop/v6/Cart?u=w&extra=deliveryAddress%2CdeliveryDate", nil)
	if err != nil {
		return 0, false, "", fail(step, "request")
	}
	req.Header.Set("jwt_token", b.jwt)
	_, data, err := b.a.do(b.c, step, req, modeJSON)
	if err != nil {
		return 0, false, "", err
	}
	m, err := obj(step, data)
	if err != nil {
		return 0, false, "", err
	}
	if num(m["code"]) == 1 {
		return 0, false, "", fail(step, "not_logged_in")
	}
	if m["msg"] != nil && m["code"] != nil {
		return 0, false, "", fail(step, "refused")
	}
	for _, g := range list(m["lines"]) {
		if h := str(dig(g, "headline")); h == "Emballage" || h == "Services" {
			continue
		}
		for _, blk := range list(dig(g, "lines")) {
			lines += len(list(dig(blk, "orderlines")))
		}
	}
	addr := dig(m, "deliveryAddress")
	return lines, truthy(dig(addr, "dawa_uid")) || truthy(dig(addr, "recipient_name")), str(dig(addr, "sid")), nil
}

func (b *bilkaRun) search(sid string) (int, error) {
	attrs, _ := json.Marshal([]string{"name", "brand", "gtin", "netcontent", "productType",
		"deepestCategoryPath", "price", "storeData." + sid})
	params := url.Values{"query": {"mælk"}, "hitsPerPage": {"3"}, "removeWordsIfNoResults": {"allOptional"},
		"filters": {"isInAssortmentIn:" + sid + " AND nonsearchable:false"}, "attributesToRetrieve": {string(attrs)}}
	body, _ := json.Marshal(map[string]any{"requests": []any{
		map[string]string{"indexName": algoliaIdx, "params": params.Encode()}}})
	req, err := http.NewRequest("POST", algoliaURL, strings.NewReader(string(body)))
	if err != nil {
		return 0, fail("search", "request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-algolia-application-id", algoliaApp)
	req.Header.Set("x-algolia-api-key", algoliaKey)
	_, data, err := b.a.do(b.c, "search", req, modeJSON)
	if err != nil {
		return 0, err
	}
	results := list(dig(data, "results"))
	if len(results) == 0 {
		return 0, fail("search", "unexpected_shape")
	}
	hits, ok := dig(results[0], "hits").([]any)
	if !ok {
		return 0, fail("search", "unexpected_shape")
	}
	return len(hits), nil
}

func (a *app) bilka(email, password string) error {
	b := &bilkaRun{a: a, c: a.newClient(false, bilkaLimit)}
	m, err := b.gigya("login", "accounts.login", url.Values{
		"loginID": {email}, "password": {password}, "apiKey": {gigyaKey}, "httpStatusCodes": {"true"}})
	if err != nil {
		return err
	}
	token := str(dig(m, "sessionInfo", "cookieValue"))
	if token == "" {
		return fail("login", "unexpected_shape")
	}
	jwt, err := b.jwtFor("getjwt", token)
	if err != nil {
		return err
	}
	b.jwt = jwt
	if err := b.loginJWT(jwt); err != nil {
		return err
	}
	lines, home, sid, err := b.cart("cart")
	if err != nil {
		return err
	}
	a.say("line_count=%d home_delivery=%t sid_present=%t", lines, home, sid != "")
	if sid == "" {
		return fail("cart", "no_address")
	}
	hits, err := b.search(sid)
	if err != nil {
		return err
	}
	a.say("search_hits=%d", hits)
	a.say("session_json_bytes=%d", sessionBytes(email, map[string]string{"login_token": token}))

	// Resume: a new client with an empty jar, the login token only.
	r := &bilkaRun{a: a, c: a.newClient(false, bilkaLimit)}
	jwt, err = r.jwtFor("resume_getjwt", token)
	if err != nil {
		return err
	}
	r.jwt = jwt
	if err := r.loginJWT(jwt); err != nil {
		return err
	}
	lines, _, _, err = r.cart("resume_cart")
	if err != nil {
		return err
	}
	a.say("final_line_count=%d", lines)
	return nil
}
