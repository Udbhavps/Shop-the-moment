package main

// A small merchant ("business") implementing the cart and checkout capabilities
// of the Universal Commerce Protocol (UCP), REST binding, spec version 2026-04-08.
// Spec: https://ucp.dev/2026-04-08/specification/overview/
//
// Scope, stated plainly:
//   - Implemented: /.well-known/ucp profile, cart (create/get/update/cancel),
//     checkout (create/get/update/complete/cancel), status lifecycle, totals in
//     minor units, idempotency keys, UCP-Agent / Request-Id headers, business
//     errors as HTTP 200 with a messages[] array.
//   - Not implemented: request/response signing (RFC 9421), fulfillment and
//     discount extensions, identity linking, fetching the platform's profile.
//   - Payment is a made-up handler ("demo_pay"). No money moves anywhere.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ucpVersion       = "2026-04-08"
	ucpDemoHandler   = "demo_pay"
	ucpHandlerName   = "dev.shop_the_moment.demo_pay"
	ucpCapCheckout   = "dev.ucp.shopping.checkout"
	ucpCapCart       = "dev.ucp.shopping.cart"
	ucpCartTTL       = 24 * time.Hour
	ucpCheckoutTTL   = 6 * time.Hour
	ucpMaxLines      = 20
	ucpMaxQty        = 9
	ucpMaxCarts      = 5000
	ucpIdemTTL       = 24 * time.Hour
	ucpMaxIdemKeys   = 5000
	ucpDemoTokenHead = "demo_tok_"
)

// ---------------------------------------------------------------- wire types ---

type ucpCapRef struct {
	Version string `json:"version"`
}

type ucpHandlerRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ucpMeta struct {
	Version         string                     `json:"version"`
	Status          string                     `json:"status"` // success | error
	Capabilities    map[string][]ucpCapRef     `json:"capabilities,omitempty"`
	PaymentHandlers map[string][]ucpHandlerRef `json:"payment_handlers,omitempty"`
}

type ucpTotal struct {
	Type        string `json:"type"`
	Amount      int64  `json:"amount"` // minor units (cents)
	DisplayText string `json:"display_text,omitempty"`
}

type ucpItem struct {
	ID       string `json:"id"` // the variant SKU
	Title    string `json:"title"`
	Price    int64  `json:"price"`
	ImageURL string `json:"image_url,omitempty"`
}

type ucpLine struct {
	ID       string     `json:"id"`
	Item     ucpItem    `json:"item"`
	Quantity int        `json:"quantity"`
	Totals   []ucpTotal `json:"totals"`
}

type ucpMessage struct {
	Type     string `json:"type"` // error | warning | info
	Code     string `json:"code"`
	Content  string `json:"content"`
	Severity string `json:"severity,omitempty"` // recoverable | requires_buyer_input | requires_buyer_review | unrecoverable
	Path     string `json:"path,omitempty"`
}

type ucpLink struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type ucpInstrument struct {
	ID         string         `json:"id"`
	HandlerID  string         `json:"handler_id"`
	Type       string         `json:"type"`
	Selected   bool           `json:"selected,omitempty"`
	Display    map[string]any `json:"display,omitempty"`
	Credential *ucpCredential `json:"credential,omitempty"`
}

type ucpCredential struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type ucpPayment struct {
	Instruments []ucpInstrument `json:"instruments"`
}

type ucpOrder struct {
	ID           string `json:"id"`
	PermalinkURL string `json:"permalink_url"`
}

type ucpCartResp struct {
	UCP       ucpMeta      `json:"ucp"`
	ID        string       `json:"id,omitempty"`
	LineItems []ucpLine    `json:"line_items,omitempty"`
	Currency  string       `json:"currency,omitempty"`
	Totals    []ucpTotal   `json:"totals,omitempty"`
	Messages  []ucpMessage `json:"messages,omitempty"`
	ExpiresAt string       `json:"expires_at,omitempty"`
}

type ucpCheckoutResp struct {
	UCP       ucpMeta      `json:"ucp"`
	ID        string       `json:"id,omitempty"`
	Status    string       `json:"status,omitempty"` // incomplete | requires_escalation | ready_for_complete | complete_in_progress | completed | canceled
	CartID    string       `json:"cart_id,omitempty"`
	LineItems []ucpLine    `json:"line_items,omitempty"`
	Currency  string       `json:"currency,omitempty"`
	Totals    []ucpTotal   `json:"totals,omitempty"`
	Messages  []ucpMessage `json:"messages,omitempty"`
	Links     []ucpLink    `json:"links,omitempty"`
	Payment   *ucpPayment  `json:"payment,omitempty"`
	Order     *ucpOrder    `json:"order,omitempty"`
	ExpiresAt string       `json:"expires_at,omitempty"`
}

// Request shapes.
type ucpReqItem struct {
	ID string `json:"id"`
}
type ucpReqLine struct {
	Item     ucpReqItem `json:"item"`
	Quantity int        `json:"quantity"`
}
type ucpCartReq struct {
	LineItems []ucpReqLine `json:"line_items"`
}
type ucpCheckoutReq struct {
	CartID    string       `json:"cart_id"`
	LineItems []ucpReqLine `json:"line_items"`
	Payment   *ucpPayment  `json:"payment"`
}

// ------------------------------------------------------------------ business ---

type ucpStoredLine struct {
	SKU string
	Qty int
}

type ucpCart struct {
	ID      string
	Lines   []ucpStoredLine
	Expires time.Time
}

type ucpCheckout struct {
	ID      string
	CartID  string
	Lines   []ucpStoredLine
	Status  string
	Order   *ucpOrder
	Expires time.Time
	Total   int64
}

type ucpOrderRecord struct {
	ID       string
	Total    int64
	Count    int
	Titles   []string
	PlacedAt time.Time
}

type ucpIdem struct {
	fingerprint string
	status      int
	body        []byte
	at          time.Time
}

type UCP struct {
	mu        sync.Mutex
	cat       *Catalog
	now       func() time.Time
	carts     map[string]*ucpCart
	checkouts map[string]*ucpCheckout
	orders    map[string]*ucpOrderRecord
	idem      map[string]ucpIdem
}

func NewUCP(cat *Catalog) *UCP {
	return &UCP{cat: cat, now: time.Now, carts: map[string]*ucpCart{}, checkouts: map[string]*ucpCheckout{}, orders: map[string]*ucpOrderRecord{}, idem: map[string]ucpIdem{}}
}

func cents(v float64) int64 { return int64(math.Round(v * 100)) }

func (u *UCP) meta(status string, caps ...string) ucpMeta {
	m := ucpMeta{Version: ucpVersion, Status: status, Capabilities: map[string][]ucpCapRef{}}
	for _, c := range caps {
		m.Capabilities[c] = []ucpCapRef{{Version: ucpVersion}}
	}
	return m
}

func (u *UCP) checkoutMeta(status string) ucpMeta {
	m := u.meta(status, ucpCapCheckout, ucpCapCart)
	m.PaymentHandlers = map[string][]ucpHandlerRef{ucpHandlerName: {{ID: ucpDemoHandler, Version: ucpVersion}}}
	return m
}

func (u *UCP) gcLocked() {
	now := u.now()
	for id, c := range u.carts {
		if now.After(c.Expires) {
			delete(u.carts, id)
		}
	}
	for id, c := range u.checkouts {
		if c.Status != "completed" && now.After(c.Expires) {
			delete(u.checkouts, id)
		}
	}
	for k, e := range u.idem {
		if now.Sub(e.at) > ucpIdemTTL {
			delete(u.idem, k)
		}
	}
	if len(u.idem) > ucpMaxIdemKeys { // drop the oldest keys
		type kv struct {
			k string
			t time.Time
		}
		all := make([]kv, 0, len(u.idem))
		for k, e := range u.idem {
			all = append(all, kv{k, e.at})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		for _, e := range all[:len(all)-ucpMaxIdemKeys] {
			delete(u.idem, e.k)
		}
	}
}

// resolve turns request lines into stored lines, or explains why not.
func (u *UCP) resolve(in []ucpReqLine, allowEmpty bool) ([]ucpStoredLine, []ucpMessage) {
	var msgs []ucpMessage
	if len(in) == 0 && !allowEmpty {
		return nil, []ucpMessage{{Type: "error", Code: "invalid_request", Content: "line_items must contain at least one item.", Severity: "unrecoverable", Path: "$.line_items"}}
	}
	if len(in) > ucpMaxLines {
		return nil, []ucpMessage{{Type: "error", Code: "too_many_items", Content: fmt.Sprintf("At most %d line items are allowed.", ucpMaxLines), Severity: "unrecoverable", Path: "$.line_items"}}
	}
	var out []ucpStoredLine
	seen := map[string]int{}
	for i, l := range in {
		path := fmt.Sprintf("$.line_items[%d]", i)
		p, v, ok := u.cat.LookupSKU(l.Item.ID)
		switch {
		case !ok:
			msgs = append(msgs, ucpMessage{Type: "error", Code: "item_not_found", Content: "No such item: " + truncate(l.Item.ID, 60), Severity: "unrecoverable", Path: path + ".item.id"})
		case !v.InStock:
			msgs = append(msgs, ucpMessage{Type: "error", Code: "out_of_stock", Content: p.Name + " (" + v.Color + ") is out of stock.", Severity: "unrecoverable", Path: path})
		case l.Quantity < 1 || l.Quantity > ucpMaxQty:
			msgs = append(msgs, ucpMessage{Type: "error", Code: "invalid_quantity", Content: fmt.Sprintf("Quantity must be between 1 and %d.", ucpMaxQty), Severity: "recoverable", Path: path + ".quantity"})
		default:
			if j, dup := seen[v.SKU]; dup { // merge repeated SKUs
				out[j].Qty += l.Quantity
				if out[j].Qty > ucpMaxQty {
					msgs = append(msgs, ucpMessage{Type: "error", Code: "invalid_quantity", Content: fmt.Sprintf("Quantity must be between 1 and %d.", ucpMaxQty), Severity: "recoverable", Path: path + ".quantity"})
				}
			} else {
				seen[v.SKU] = len(out)
				out = append(out, ucpStoredLine{SKU: v.SKU, Qty: l.Quantity})
			}
		}
	}
	if len(msgs) > 0 {
		return nil, msgs
	}
	return out, nil
}

func (u *UCP) render(lines []ucpStoredLine) ([]ucpLine, []ucpTotal) {
	var out []ucpLine
	var sum int64
	for i, l := range lines {
		p, v, ok := u.cat.LookupSKU(l.SKU)
		if !ok {
			continue
		}
		price := cents(p.Price)
		amt := price * int64(l.Qty)
		sum += amt
		out = append(out, ucpLine{
			ID:       fmt.Sprintf("li_%d", i+1),
			Item:     ucpItem{ID: v.SKU, Title: p.Name + " (" + v.Color + ")", Price: price, ImageURL: "/img/" + p.ID + ".svg?color=" + v.Color},
			Quantity: l.Qty,
			Totals:   []ucpTotal{{Type: "subtotal", Amount: amt}, {Type: "total", Amount: amt}},
		})
	}
	// Tax and shipping are not modelled in this demo, so total equals subtotal.
	return out, []ucpTotal{{Type: "subtotal", Amount: sum}, {Type: "total", Amount: sum, DisplayText: "Total (demo: no tax or shipping)"}}
}

func (u *UCP) cartResp(c *ucpCart) ucpCartResp {
	lines, totals := u.render(c.Lines)
	return ucpCartResp{UCP: u.meta("success", ucpCapCart), ID: c.ID, LineItems: lines, Currency: u.cat.Currency, Totals: totals, ExpiresAt: c.Expires.UTC().Format(time.RFC3339)}
}

func (u *UCP) cartErr(msgs ...ucpMessage) ucpCartResp {
	return ucpCartResp{UCP: u.meta("error", ucpCapCart), Messages: msgs}
}

func ucpNotFound(what string) ucpMessage {
	return ucpMessage{Type: "error", Code: "not_found", Content: what + " was not found, has expired or was canceled.", Severity: "unrecoverable"}
}

// ---- cart ----

func (u *UCP) CreateCart(in ucpCartReq) ucpCartResp {
	lines, msgs := u.resolve(in.LineItems, false)
	if msgs != nil {
		return u.cartErr(msgs...)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.gcLocked()
	if len(u.carts) >= ucpMaxCarts {
		return u.cartErr(ucpMessage{Type: "error", Code: "capacity", Content: "The demo merchant is full. Try again later.", Severity: "unrecoverable"})
	}
	c := &ucpCart{ID: "cart_" + newID(12), Lines: lines, Expires: u.now().Add(ucpCartTTL)}
	u.carts[c.ID] = c
	return u.cartResp(c)
}

func (u *UCP) GetCart(id string) ucpCartResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.carts[id]
	if !ok || u.now().After(c.Expires) {
		return u.cartErr(ucpNotFound("Cart"))
	}
	return u.cartResp(c)
}

// UpdateCart replaces the whole cart (spec: "MUST send the entire cart resource").
// An empty line_items list is accepted here so a shopper can remove the last item.
func (u *UCP) UpdateCart(id string, in ucpCartReq) ucpCartResp {
	lines, msgs := u.resolve(in.LineItems, true)
	if msgs != nil {
		return u.cartErr(msgs...)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.carts[id]
	if !ok || u.now().After(c.Expires) {
		return u.cartErr(ucpNotFound("Cart"))
	}
	c.Lines = lines
	c.Expires = u.now().Add(ucpCartTTL)
	return u.cartResp(c)
}

func (u *UCP) CancelCart(id string) ucpCartResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.carts[id]
	if !ok || u.now().After(c.Expires) {
		return u.cartErr(ucpNotFound("Cart"))
	}
	resp := u.cartResp(c) // state before deletion
	delete(u.carts, id)
	return resp
}

// ---- checkout ----

func (u *UCP) checkoutErr(msgs ...ucpMessage) ucpCheckoutResp {
	return ucpCheckoutResp{UCP: u.checkoutMeta("error"), Messages: msgs}
}

func (u *UCP) checkoutResp(c *ucpCheckout, base string) ucpCheckoutResp {
	lines, totals := u.render(c.Lines)
	r := ucpCheckoutResp{UCP: u.checkoutMeta("success"), ID: c.ID, Status: c.Status, CartID: c.CartID, LineItems: lines, Currency: u.cat.Currency, Totals: totals, Order: c.Order,
		ExpiresAt: c.Expires.UTC().Format(time.RFC3339),
		Links:     []ucpLink{{Type: "terms_of_service", URL: base + "/ucp/handlers/" + ucpDemoHandler, Title: "Demo only: nothing is charged"}}}
	if c.Status == "ready_for_complete" || c.Status == "incomplete" {
		r.Payment = &ucpPayment{Instruments: []ucpInstrument{{
			ID: "demo_card_4242", HandlerID: ucpDemoHandler, Type: "demo_token", Selected: true,
			Display: map[string]any{"brand": "Demo card", "last_digits": "4242", "description": "Simulated payment. No money moves."},
		}}}
	}
	return r
}

func (u *UCP) CreateCheckout(in ucpCheckoutReq, base string) ucpCheckoutResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.gcLocked()
	var lines []ucpStoredLine
	if in.CartID != "" {
		c, ok := u.carts[in.CartID]
		if !ok || u.now().After(c.Expires) {
			return u.checkoutErr(ucpNotFound("Cart"))
		}
		// Spec: if an incomplete checkout already exists for the cart, return it.
		for _, co := range u.checkouts {
			if co.CartID == in.CartID && (co.Status == "incomplete" || co.Status == "ready_for_complete") {
				co.Lines = append([]ucpStoredLine(nil), c.Lines...)
				co.Total = u.total(co.Lines)
				return u.checkoutResp(co, base)
			}
		}
		lines = append([]ucpStoredLine(nil), c.Lines...)
		if len(lines) == 0 {
			return u.checkoutErr(ucpMessage{Type: "error", Code: "cart_empty", Content: "The cart is empty.", Severity: "unrecoverable"})
		}
	} else {
		var msgs []ucpMessage
		lines, msgs = u.resolve(in.LineItems, false) // reads only the immutable catalog
		if msgs != nil {
			return u.checkoutErr(msgs...)
		}
	}
	co := &ucpCheckout{ID: "chk_" + newID(12), CartID: in.CartID, Lines: lines, Status: "ready_for_complete", Expires: u.now().Add(ucpCheckoutTTL)}
	co.Total = u.total(lines)
	u.checkouts[co.ID] = co
	return u.checkoutResp(co, base)
}

func (u *UCP) total(lines []ucpStoredLine) int64 {
	_, t := u.render(lines)
	return t[len(t)-1].Amount
}

func (u *UCP) GetCheckout(id, base string) ucpCheckoutResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.checkouts[id]
	if !ok || (c.Status != "completed" && u.now().After(c.Expires)) {
		return u.checkoutErr(ucpNotFound("Checkout"))
	}
	return u.checkoutResp(c, base)
}

// UpdateCheckout replaces the line items (when given). Payment is only collected by Complete.
func (u *UCP) UpdateCheckout(id string, in ucpCheckoutReq, base string) ucpCheckoutResp {
	var lines []ucpStoredLine
	if in.LineItems != nil {
		var msgs []ucpMessage
		lines, msgs = u.resolve(in.LineItems, false)
		if msgs != nil {
			return u.checkoutErr(msgs...)
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.checkouts[id]
	if !ok || (c.Status != "completed" && u.now().After(c.Expires)) {
		return u.checkoutErr(ucpNotFound("Checkout"))
	}
	if c.Status == "completed" || c.Status == "canceled" {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "checkout_closed", Content: "This checkout is " + c.Status + " and can no longer change.", Severity: "unrecoverable"})
	}
	if in.LineItems != nil {
		c.Lines = lines
		c.Total = u.total(lines)
	}
	return u.checkoutResp(c, base)
}

func (u *UCP) CompleteCheckout(id string, in ucpCheckoutReq, base string) ucpCheckoutResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.checkouts[id]
	if !ok || (c.Status != "completed" && u.now().After(c.Expires)) {
		return u.checkoutErr(ucpNotFound("Checkout"))
	}
	if c.Status != "ready_for_complete" {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "checkout_not_ready", Content: "This checkout is " + c.Status + ", so it cannot be completed.", Severity: "unrecoverable"})
	}
	if in.Payment == nil || len(in.Payment.Instruments) != 1 {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "payment_required", Content: "Send exactly one payment instrument.", Severity: "requires_buyer_input", Path: "$.payment.instruments"})
	}
	ins := in.Payment.Instruments[0]
	if ins.HandlerID != ucpDemoHandler {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "unsupported_handler", Content: "Only the demo_pay handler is supported.", Severity: "requires_buyer_input", Path: "$.payment.instruments[0].handler_id"})
	}
	if ins.Credential == nil || !strings.HasPrefix(ins.Credential.Token, ucpDemoTokenHead) {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "payment_credential_invalid", Content: "The demo payment token is missing or invalid. No payment was taken.", Severity: "requires_buyer_input", Path: "$.payment.instruments[0].credential"})
	}
	orderID := "DEMO-" + upper(newID(6))
	c.Status = "completed"
	c.Order = &ucpOrder{ID: orderID, PermalinkURL: base + "/order/" + orderID}
	rec := &ucpOrderRecord{ID: orderID, Total: u.total(c.Lines), PlacedAt: u.now()}
	for _, l := range c.Lines {
		if p, v, ok := u.cat.LookupSKU(l.SKU); ok {
			rec.Count += l.Qty
			rec.Titles = append(rec.Titles, fmt.Sprintf("%d x %s (%s)", l.Qty, p.Name, v.Color))
		}
	}
	u.orders[orderID] = rec
	delete(u.carts, c.CartID) // the cart has become an order
	return u.checkoutResp(c, base)
}

func (u *UCP) CancelCheckout(id, base string) ucpCheckoutResp {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.checkouts[id]
	if !ok {
		return u.checkoutErr(ucpNotFound("Checkout"))
	}
	if c.Status == "completed" || c.Status == "canceled" {
		return u.checkoutErr(ucpMessage{Type: "error", Code: "checkout_closed", Content: "This checkout is already " + c.Status + ".", Severity: "unrecoverable"})
	}
	c.Status = "canceled"
	return u.checkoutResp(c, base)
}

func (u *UCP) Order(id string) (ucpOrderRecord, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	o, ok := u.orders[id]
	if !ok {
		return ucpOrderRecord{}, false
	}
	return *o, true
}

// ---- helpers shared with the session adapter ----

// CartLines returns the SKU/quantity pairs of a live cart (nil if it is gone).
func (u *UCP) CartLines(id string) []ucpStoredLine {
	u.mu.Lock()
	defer u.mu.Unlock()
	c, ok := u.carts[id]
	if !ok || u.now().After(c.Expires) {
		return nil
	}
	return append([]ucpStoredLine(nil), c.Lines...)
}

func fingerprint(method, path string, body []byte) string {
	h := sha256.Sum256(append([]byte(method+" "+path+"\n"), body...))
	return hex.EncodeToString(h[:])
}
