package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	nemligBase  = "https://" + nemligHost
	nemligSrch  = "https://" + searchHost + "/searchgateway/api/search"
	appVersion  = "11.201.0"
	authCookie  = ".ASPXAUTH"
	nemligLimit = 20 * time.Second
)

type nemligRun struct {
	a            *app
	c            *http.Client
	bearer, xsrf string
}

func (n *nemligRun) call(step, method, rawURL string, body any) (any, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, rawURL, rd)
	if err != nil {
		return nil, fail(step, "request")
	}
	h := req.Header
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("Device-Size", "desktop")
	h.Set("Platform", "web")
	h.Set("Version", appVersion)
	h.Set("X-Correlation-Id", uuid4())
	h.Set("Referer", nemligBase+"/")
	h.Set("User-Agent", "Mozilla/5.0 (handle-mcp)")
	if n.bearer != "" {
		h.Set("Authorization", "Bearer "+n.bearer)
	}
	if n.xsrf != "" {
		h.Set("X-XSRF-TOKEN", n.xsrf)
	}
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	_, data, err := n.a.do(n.c, step, req, modeJSON)
	return data, err
}

// renew mirrors _renew_token: the Bearer token is dropped first, the XSRF token is kept.
func (n *nemligRun) renew(stage string) error {
	n.bearer = ""
	tok, err := n.call(stage+"_token", "GET", nemligBase+"/webapi/Token", nil)
	if err != nil {
		return err
	}
	if n.bearer = str(dig(tok, "access_token")); n.bearer == "" {
		return fail(stage+"_token", "unexpected_shape")
	}
	x, err := n.call(stage+"_antiforgery", "GET", nemligBase+"/webapi/AntiForgery", nil)
	if err != nil {
		return err
	}
	if n.xsrf = str(dig(x, "Value")); n.xsrf == "" {
		return fail(stage+"_antiforgery", "unexpected_shape")
	}
	return nil
}

// basket reads the cart. Lines with quantity 0 are not counted, as in _cart.
func (n *nemligRun) basket(step string) (emailPresent bool, lines int, m map[string]any, err error) {
	d, err := n.call(step, "GET", nemligBase+"/webapi/basket/GetBasket", nil)
	if err != nil {
		return false, 0, nil, err
	}
	if m, err = obj(step, d); err != nil {
		return false, 0, nil, err
	}
	for _, ln := range list(m["Lines"]) {
		if num(dig(ln, "Quantity")) > 0 {
			lines++
		}
	}
	return truthy(m["Email"]), lines, m, nil
}

func (a *app) nemlig(email, password string) error {
	n := &nemligRun{a: a, c: a.newClient(true, nemligLimit)}
	if err := n.renew("pre"); err != nil {
		return err
	}
	login := map[string]any{"Username": email, "Password": password,
		"CheckForExistingProducts": true, "DoMerge": true, "AppInstalled": false, "SaveExistingBasket": false}
	res, err := n.call("login", "POST", nemligBase+"/webapi/login", login)
	if err != nil {
		return err
	}
	if m, ok := res.(map[string]any); !ok || m["RedirectUrl"] == nil {
		a.say("login_ok=false")
		return fail("login", "rejected")
	}
	a.say("login_ok=true")
	if err := n.renew("post"); err != nil {
		return err
	}

	has, lines, basket, err := n.basket("basket")
	if err != nil {
		return err
	}
	a.say("email_present=%t line_count=%d", has, lines)

	app, err := n.call("appsettings", "GET", nemligBase+"/webapi/v2/AppSettings/Website", nil)
	if err != nil {
		return err
	}
	zone := str(basket["DeliveryZoneId"])
	if !truthy(basket["DeliveryZoneId"]) {
		zone = "1"
	}
	q := url.Values{"query": {"mælk"}, "take": {"3"}, "skip": {"0"}, "recipeCount": {"0"},
		"timestamp":      {str(dig(app, "CombinedProductsAndSitecoreTimestamp"))},
		"timeslotUtc":    {str(basket["TimeslotUtc"])},
		"deliveryZoneId": {zone}}
	hits, err := n.call("search", "GET", nemligSrch+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	products, ok := dig(hits, "Products", "Products").([]any)
	if !ok {
		return fail("search", "unexpected_shape")
	}
	a.say("search_hits=%d", len(products))

	// The saved session is the .ASPXAUTH cookie only.
	u, _ := url.Parse(nemligBase)
	cookie := ""
	for _, c := range n.c.Jar.Cookies(u) {
		if c.Name == authCookie {
			cookie = c.Value
		}
	}
	a.say("session_cookie_present=%t", cookie != "")
	if cookie == "" {
		return fail("session", "no_cookie")
	}
	a.say("session_json_bytes=%d", sessionBytes(email, map[string]string{authCookie: cookie}))

	// Resume: a fresh client and jar, only the login cookie.
	r := &nemligRun{a: a, c: a.newClient(true, nemligLimit)}
	r.c.Jar.SetCookies(u, []*http.Cookie{{Name: authCookie, Value: cookie, Domain: ".nemlig.com", Path: "/"}})
	if err := r.renew("resume"); err != nil {
		return err
	}
	has, lines, _, err = r.basket("resume_basket")
	if err != nil {
		return err
	}
	a.say("resume_email_present=%t", has)
	if !has {
		return fail("resume_basket", "resume_failed")
	}
	a.say("final_line_count=%d", lines)
	return nil
}
