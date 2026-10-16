package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testAgent = `profile="https://platform.example/.well-known/ucp"`

// ucpCall sends a UCP request with the headers a platform must send.
func ucpCall(t *testing.T, method, url string, idem string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	var rd io.Reader = bytes.NewReader(nil)
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("UCP-Agent", testAgent)
	req.Header.Set("Request-Id", "test-req-1")
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out, resp.Header
}

func totalOf(m map[string]any) float64 {
	ts := m["totals"].([]any)
	return ts[len(ts)-1].(map[string]any)["amount"].(float64)
}

func TestUCPProfileAndHeaders(t *testing.T) {
	ts, _ := testServer(t, nil)
	code, out := call(t, "GET", ts.URL+"/.well-known/ucp", nil)
	u := out["ucp"].(map[string]any)
	if code != 200 || u["version"] != ucpVersion {
		t.Fatalf("profile: %d %v", code, out)
	}
	svc := u["services"].(map[string]any)["dev.ucp.shopping"].([]any)[0].(map[string]any)
	if svc["transport"] != "rest" || !strings.HasSuffix(svc["endpoint"].(string), "/ucp/v1") {
		t.Fatalf("bad service: %v", svc)
	}
	caps := u["capabilities"].(map[string]any)
	if caps[ucpCapCheckout] == nil || caps[ucpCapCart] == nil {
		t.Fatalf("missing capabilities: %v", caps)
	}
	if _, ok := out["keys"]; !ok {
		t.Fatal("profile must have a keys array (empty: signing not implemented)")
	}

	// Missing UCP-Agent is a protocol error.
	req, _ := http.NewRequest("POST", ts.URL+"/ucp/v1/carts", strings.NewReader(`{}`))
	req.Header.Set("Idempotency-Key", "k")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 400 {
		t.Fatalf("no UCP-Agent should be 400, got %d", resp.StatusCode)
	}
	// Missing Idempotency-Key on create is a protocol error.
	if code, _, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "", ucpCartReq{}); code != 400 {
		t.Fatalf("no Idempotency-Key should be 400, got %d", code)
	}
}

func TestUCPCartLifecycle(t *testing.T) {
	ts, _ := testServer(t, nil)
	line := func(sku string, q int) ucpReqLine { return ucpReqLine{Item: ucpReqItem{ID: sku}, Quantity: q} }

	code, c, h := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "c1", ucpCartReq{LineItems: []ucpReqLine{line("STRIDE-PULSE-WHITE", 2), line("HYDRA-STEEL-ORANGE", 1)}})
	if code != 201 || c["id"] == nil || h.Get("Request-Id") != "test-req-1" {
		t.Fatalf("create: %d %v", code, c)
	}
	if totalOf(c) != 2*999900+249900 || c["currency"] != "INR" {
		t.Fatalf("totals in minor units: %v", c["totals"])
	}
	id := c["id"].(string)

	// Same key + same body replays; same key + different body conflicts.
	code, c2, h2 := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "c1", ucpCartReq{LineItems: []ucpReqLine{line("STRIDE-PULSE-WHITE", 2), line("HYDRA-STEEL-ORANGE", 1)}})
	if code != 201 || c2["id"] != id || h2.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %v", code, c2)
	}
	if code, _, _ = ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "c1", ucpCartReq{LineItems: []ucpReqLine{line("STRIDE-PULSE-WHITE", 1)}}); code != 409 {
		t.Fatalf("reused key with a different body must be 409, got %d", code)
	}

	// Update replaces the whole cart.
	code, c, _ = ucpCall(t, "PUT", ts.URL+"/ucp/v1/carts/"+id, "", ucpCartReq{LineItems: []ucpReqLine{line("HYDRA-STEEL-ORANGE", 1)}})
	if code != 200 || totalOf(c) != 249900 || len(c["line_items"].([]any)) != 1 {
		t.Fatalf("update: %d %v", code, c)
	}
	// Business errors are HTTP 200 with ucp.status=error and no id.
	for _, tc := range []struct{ sku, code string }{{"NOPE-BLACK", "item_not_found"}, {"STRIDE-PULSE-RED", "item_not_found"}} {
		code, e, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "bad-"+tc.sku, ucpCartReq{LineItems: []ucpReqLine{line(tc.sku, 1)}})
		msg := e["messages"].([]any)[0].(map[string]any)
		if code != 200 || e["id"] != nil || e["ucp"].(map[string]any)["status"] != "error" || msg["code"] != tc.code {
			t.Fatalf("%s: %d %v", tc.sku, code, e)
		}
	}
	if code, e, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "q0", ucpCartReq{LineItems: []ucpReqLine{line("STRIDE-PULSE-WHITE", 0)}}); code != 200 || e["messages"].([]any)[0].(map[string]any)["code"] != "invalid_quantity" {
		t.Fatalf("quantity 0 must be refused: %d %v", code, e)
	}
	// Cancel returns the state before deletion, then the cart is gone.
	code, c, _ = ucpCall(t, "POST", ts.URL+"/ucp/v1/carts/"+id+"/cancel", "", nil)
	if code != 200 || c["id"] != id {
		t.Fatalf("cancel: %d %v", code, c)
	}
	code, c, _ = ucpCall(t, "GET", ts.URL+"/ucp/v1/carts/"+id, "", nil)
	if code != 200 || c["messages"].([]any)[0].(map[string]any)["code"] != "not_found" {
		t.Fatalf("get after cancel: %d %v", code, c)
	}
}

func TestUCPCheckoutLifecycle(t *testing.T) {
	ts, s := testServer(t, nil)
	_, cart, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "k-cart", ucpCartReq{LineItems: []ucpReqLine{{Item: ucpReqItem{ID: "KORVO-HALO-BLACK"}, Quantity: 1}}})
	cartID := cart["id"].(string)

	code, co, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions", "k-co", ucpCheckoutReq{CartID: cartID})
	if code != 201 || co["status"] != "ready_for_complete" || totalOf(co) != 1599900 {
		t.Fatalf("create checkout: %d %v", code, co)
	}
	if handlers := co["ucp"].(map[string]any)["payment_handlers"].(map[string]any); handlers[ucpHandlerName] == nil {
		t.Fatalf("checkout must advertise payment handlers: %v", co["ucp"])
	}
	coID := co["id"].(string)

	// A second create for the same cart returns the open checkout.
	_, again, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions", "k-co-2", ucpCheckoutReq{CartID: cartID})
	if again["id"] != coID {
		t.Fatalf("open checkout for the cart must be reused: %v vs %v", again["id"], coID)
	}

	complete := func(key string, ins ucpInstrument) (int, map[string]any) {
		code, out, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions/"+coID+"/complete", key, ucpCheckoutReq{Payment: &ucpPayment{Instruments: []ucpInstrument{ins}}})
		return code, out
	}
	good := ucpInstrument{ID: "demo_card_4242", HandlerID: ucpDemoHandler, Type: "demo_token", Credential: &ucpCredential{Type: "demo_token", Token: "demo_tok_abc"}}

	// Wrong handler and bad token are refused and nothing is charged or completed.
	bad := good
	bad.HandlerID = "shop_pay"
	if code, out := complete("k-c1", bad); code != 200 || out["messages"].([]any)[0].(map[string]any)["code"] != "unsupported_handler" {
		t.Fatalf("wrong handler: %d %v", code, out)
	}
	bad = good
	bad.Credential = &ucpCredential{Type: "card", Token: "4242424242424242"}
	if code, out := complete("k-c2", bad); code != 200 || out["messages"].([]any)[0].(map[string]any)["code"] != "payment_credential_invalid" {
		t.Fatalf("real-looking card data must be refused: %d %v", code, out)
	}
	if _, g, _ := ucpCall(t, "GET", ts.URL+"/ucp/v1/checkout-sessions/"+coID, "", nil); g["status"] != "ready_for_complete" {
		t.Fatalf("failed payments must not change status: %v", g["status"])
	}

	code, out := complete("k-c3", good)
	order, _ := out["order"].(map[string]any)
	if code != 200 || out["status"] != "completed" || order == nil || !strings.HasPrefix(order["id"].(string), "DEMO-") {
		t.Fatalf("complete: %d %v", code, out)
	}
	// Retrying with the same key replays; a new key on a completed checkout is refused.
	if code, out2 := complete("k-c3", good); code != 200 || out2["order"].(map[string]any)["id"] != order["id"] {
		t.Fatalf("idempotent replay: %d %v", code, out2)
	}
	if _, out3 := complete("k-c4", good); out3["messages"].([]any)[0].(map[string]any)["code"] != "checkout_not_ready" {
		t.Fatalf("double complete must be refused: %v", out3)
	}
	// The cart became an order; the receipt page exists.
	if _, g, _ := ucpCall(t, "GET", ts.URL+"/ucp/v1/carts/"+cartID, "", nil); g["messages"].([]any)[0].(map[string]any)["code"] != "not_found" {
		t.Fatal("cart should be consumed by the order")
	}
	resp, _ := http.Get(ts.URL + "/order/" + order["id"].(string))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "No payment was taken") {
		t.Fatalf("order page: %d %s", resp.StatusCode, body)
	}
	_ = s

	// Cancel works before completion and not after.
	_, cart2, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "k-cart-b", ucpCartReq{LineItems: []ucpReqLine{{Item: ucpReqItem{ID: "DASH-LITE-BLACK"}, Quantity: 1}}})
	_, co2, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions", "k-co-b", ucpCheckoutReq{CartID: cart2["id"].(string)})
	_, cx, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions/"+co2["id"].(string)+"/cancel", "", nil)
	if cx["status"] != "canceled" {
		t.Fatalf("cancel: %v", cx)
	}
	if _, cx2, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions/"+coID+"/cancel", "", nil); cx2["messages"].([]any)[0].(map[string]any)["code"] != "checkout_closed" {
		t.Fatalf("completed checkout cannot be canceled: %v", cx2)
	}
}

// The TV and phone endpoints are adapters over the same UCP cart and checkout.
func TestSessionCartRunsOnUCP(t *testing.T) {
	ts, s := testServer(t, nil)
	_, out := call(t, "POST", ts.URL+"/api/sessions", map[string]any{"ad_id": "morning-run"})
	sid := out["session"].(map[string]any)["id"].(string)
	cartURL := ts.URL + "/api/sessions/" + sid + "/cart"

	code, v := call(t, "POST", cartURL, map[string]any{"product_id": "stride-pulse", "color": "white", "delta": 1, "channel": "tv"})
	if code != 200 || v["cart_total"].(float64) != 9999 {
		t.Fatalf("add: %d %v", code, v)
	}
	cartID := v["session"].(map[string]any)["ucp_cart_id"].(string)
	// The same cart is visible through the UCP API.
	_, g, _ := ucpCall(t, "GET", ts.URL+"/ucp/v1/carts/"+cartID, "", nil)
	if totalOf(g) != 999900 {
		t.Fatalf("UCP cart should match: %v", g)
	}
	call(t, "POST", cartURL, map[string]any{"product_id": "stride-pulse", "color": "white", "delta": 2})
	code, v = call(t, "POST", cartURL, map[string]any{"product_id": "stride-pulse", "color": "white", "delta": -3})
	if code != 200 || len(v["cart"].([]any)) != 0 {
		t.Fatalf("removing the last item must empty the cart: %d %v", code, v)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/checkout", map[string]any{"channel": "tv"}); code != 400 {
		t.Fatalf("empty cart checkout must fail, got %d", code)
	}
	call(t, "POST", cartURL, map[string]any{"product_id": "hydra-steel", "color": "orange", "delta": 2})
	code, v = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/checkout", map[string]any{"channel": "tv"})
	ord := v["session"].(map[string]any)["order"].(map[string]any)
	if code != 200 || ord["total"].(float64) != 4998 || ord["item_count"].(float64) != 2 {
		t.Fatalf("checkout: %d %v", code, v)
	}
	if _, rec := s.ucp.Order(ord["id"].(string)); !rec {
		t.Fatal("the UCP merchant must hold the order")
	}
	var tvPurchase bool
	for _, e := range s.store.Events(0, 100) {
		if e.Name == "demo_purchase_completed" && e.Props["channel"] == "tv" {
			tvPurchase = true
		}
	}
	if !tvPurchase {
		t.Fatal("purchase event should record the TV channel")
	}
	if code, _ = call(t, "POST", cartURL, map[string]any{"product_id": "dash-lite", "delta": 1}); code != 400 {
		t.Fatalf("cart must be locked after the order, got %d", code)
	}
}

func TestUPIPaymentIntentFlow(t *testing.T) {
	ts, s := testServer(t, nil)
	_, out := call(t, "POST", ts.URL+"/api/sessions", map[string]any{"ad_id": "morning-run"})
	sid := out["session"].(map[string]any)["id"].(string)
	cartURL := ts.URL + "/api/sessions/" + sid + "/cart"
	intentURL := ts.URL + "/api/sessions/" + sid + "/pay-intent"

	if code, _ := call(t, "POST", intentURL, map[string]any{}); code != 400 {
		t.Fatalf("empty cart must not get a payment intent, got %d", code)
	}
	call(t, "POST", cartURL, map[string]any{"product_id": "korvo-halo", "color": "black", "delta": 1, "channel": "tv"})
	code, in := call(t, "POST", intentURL, map[string]any{})
	if code != 200 || in["amount"].(float64) != 15999 || in["currency"] != "INR" || in["demo"] != true {
		t.Fatalf("intent: %d %v", code, in)
	}
	pid := in["intent_id"].(string)
	if !strings.Contains(in["pay_url"].(string), "/pay/"+pid) || !strings.HasPrefix(in["qr_url"].(string), "/api/qr?text=") {
		t.Fatalf("bad urls: %v", in)
	}
	// The QR holds an https/http link to this app, never a upi:// string.
	if strings.Contains(in["pay_url"].(string), "upi://") {
		t.Fatal("QR must not carry a real UPI payment string")
	}
	// The phone page data.
	code, g := call(t, "GET", ts.URL+"/api/pay/"+pid, nil)
	lines := g["lines"].([]any)
	if code != 200 || g["status"] != "pending" || len(lines) != 1 || lines[0].(map[string]any)["name"] != "Korvo Halo ANC" {
		t.Fatalf("get intent: %d %v", code, g)
	}
	if code, _ := call(t, "GET", ts.URL+"/pay/"+pid, nil); code != 200 {
		t.Fatalf("pay page: %d", code)
	}

	// Cart changes after the QR was shown make the old QR unusable.
	call(t, "POST", cartURL, map[string]any{"product_id": "sonic-buds", "color": "black", "delta": 1, "channel": "tv"})
	if code, r := call(t, "POST", ts.URL+"/api/pay/"+pid+"/approve", map[string]any{}); code != 409 || r["status"] != "superseded" {
		t.Fatalf("stale QR must be refused: %d %v", code, r)
	}
	// A new intent for the new cart can be approved once.
	_, in = call(t, "POST", intentURL, map[string]any{})
	pid = in["intent_id"].(string)
	if in["amount"].(float64) != 18998 {
		t.Fatalf("amount should include both items: %v", in["amount"])
	}
	code, r := call(t, "POST", ts.URL+"/api/pay/"+pid+"/approve", map[string]any{})
	if code != 200 || r["status"] != "paid" || !strings.HasPrefix(r["order_id"].(string), "DEMO-") {
		t.Fatalf("approve: %d %v", code, r)
	}
	orderID := r["order_id"].(string)
	if code, r = call(t, "POST", ts.URL+"/api/pay/"+pid+"/approve", map[string]any{}); code != 409 {
		t.Fatalf("second approve must be refused: %d %v", code, r)
	}
	_, sv := call(t, "GET", ts.URL+"/api/sessions/"+sid, nil)
	ord := sv["session"].(map[string]any)["order"].(map[string]any)
	if ord["total"].(float64) != 18998 || ord["id"] != orderID {
		t.Fatalf("order: %v", ord)
	}
	var upi bool
	for _, e := range s.store.Events(0, 100) {
		if e.Name == "demo_purchase_completed" && e.Props["method"] == ucpUPIHandler && e.Props["channel"] == "upi" {
			upi = true
		}
	}
	if !upi {
		t.Fatal("purchase event should record the UPI method")
	}

	// Decline and expiry.
	_, in2 := call(t, "POST", ts.URL+"/api/sessions", map[string]any{"ad_id": "morning-run"})
	sid2 := in2["session"].(map[string]any)["id"].(string)
	call(t, "POST", ts.URL+"/api/sessions/"+sid2+"/cart", map[string]any{"product_id": "hydra-lite", "color": "green", "delta": 1})
	_, i2 := call(t, "POST", ts.URL+"/api/sessions/"+sid2+"/pay-intent", map[string]any{})
	if _, r := call(t, "POST", ts.URL+"/api/pay/"+i2["intent_id"].(string)+"/decline", map[string]any{}); r["status"] != "declined" {
		t.Fatalf("decline: %v", r)
	}
	_, i3 := call(t, "POST", ts.URL+"/api/sessions/"+sid2+"/pay-intent", map[string]any{})
	s.ucp.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	if code, r := call(t, "POST", ts.URL+"/api/pay/"+i3["intent_id"].(string)+"/approve", map[string]any{}); code != 409 || r["status"] != "expired" {
		t.Fatalf("expired intent must be refused: %d %v", code, r)
	}
	if code, _ := call(t, "GET", ts.URL+"/api/pay/pay_nope", nil); code != 404 {
		t.Fatalf("unknown intent: %d", code)
	}
}

func TestUCPUPIHandler(t *testing.T) {
	ts, _ := testServer(t, nil)
	_, cart, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/carts", "u-cart", ucpCartReq{LineItems: []ucpReqLine{{Item: ucpReqItem{ID: "SONIC-BUDS-BLACK"}, Quantity: 1}}})
	_, co, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions", "u-co", ucpCheckoutReq{CartID: cart["id"].(string)})
	id := co["id"].(string)
	// A card token does not work on the UPI handler, and the reverse.
	bad := ucpInstrument{ID: "x", HandlerID: ucpUPIHandler, Type: "demo_upi", Credential: &ucpCredential{Type: "demo_upi", Token: "demo_tok_x"}}
	_, out, _ := ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions/"+id+"/complete", "u-c1", ucpCheckoutReq{Payment: &ucpPayment{Instruments: []ucpInstrument{bad}}})
	if out["messages"].([]any)[0].(map[string]any)["code"] != "payment_credential_invalid" {
		t.Fatalf("card token on the UPI handler must fail: %v", out)
	}
	good := ucpInstrument{ID: "x", HandlerID: ucpUPIHandler, Type: "demo_upi", Credential: &ucpCredential{Type: "demo_upi", Token: ucpUPITokenHead + "abc"}}
	_, out, _ = ucpCall(t, "POST", ts.URL+"/ucp/v1/checkout-sessions/"+id+"/complete", "u-c2", ucpCheckoutReq{Payment: &ucpPayment{Instruments: []ucpInstrument{good}}})
	if out["status"] != "completed" {
		t.Fatalf("UPI complete: %v", out)
	}
	if code, doc, _ := ucpCall(t, "GET", ts.URL+"/ucp/handlers/"+ucpUPIHandler, "", nil); code != 200 || !strings.Contains(doc["description"].(string), "not connected") {
		t.Fatalf("handler doc: %d %v", code, doc)
	}
}
