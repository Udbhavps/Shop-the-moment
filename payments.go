package main

// Demo UPI payment intents.
//
// The TV shows a QR that opens /pay/{intent} on the shopper's phone. That page
// lets the shopper approve or decline. Approval completes the UCP checkout with
// the simulated "demo_upi" payment handler.
//
// This is NOT connected to UPI, a bank, a PSP or a payment gateway. The QR holds
// an https link to this app, not a upi:// payment string, so no real UPI app will
// pay it. A real integration would create the intent at a gateway (for example
// Razorpay or Cashfree) and confirm it from their webhook instead of /approve.

import (
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	payIntentTTL = 5 * time.Minute
	maxIntents   = 2000
)

type payIntent struct {
	ID        string
	SessionID string
	CartID    string
	Amount    int64  // minor units
	Status    string // pending | paid | declined | expired | superseded
	Expires   time.Time
}

type payments struct {
	mu        sync.Mutex
	m         map[string]*payIntent
	bySession map[string]string
}

func newPayments() *payments {
	return &payments{m: map[string]*payIntent{}, bySession: map[string]string{}}
}

func (p *payments) gcLocked(now time.Time) {
	for id, in := range p.m {
		if now.After(in.Expires.Add(payIntentTTL)) {
			delete(p.m, id)
		}
	}
}

func (s *Server) registerPay(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sessions/{id}/pay-intent", s.handlePayIntentCreate)
	mux.HandleFunc("GET /api/pay/{id}", s.handlePayGet)
	mux.HandleFunc("POST /api/pay/{id}/approve", func(w http.ResponseWriter, r *http.Request) { s.handlePayDecide(w, r, true) })
	mux.HandleFunc("POST /api/pay/{id}/decline", func(w http.ResponseWriter, r *http.Request) { s.handlePayDecide(w, r, false) })
}

type payLine struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Qty   int    `json:"qty"`
}

func (s *Server) payStatus(in *payIntent) string {
	if in.Status == "pending" && s.ucp.now().After(in.Expires) {
		in.Status = "expired"
	}
	return in.Status
}

func (s *Server) payLines(cartLines []ucpStoredLine) []payLine {
	var out []payLine
	for _, l := range cartLines {
		if p, v, ok := s.cat.LookupSKU(l.SKU); ok {
			out = append(out, payLine{Name: p.Name, Color: v.Color, Qty: l.Qty})
		}
	}
	return out
}

// handlePayIntentCreate (TV) starts a payment for the session's current cart.
// It replaces any earlier open intent for the session.
func (s *Server) handlePayIntentCreate(w http.ResponseWriter, r *http.Request) {
	if !s.ucpLimiter.Allow(clientIP(r)) {
		writeErr(w, 429, "too many requests, slow down")
		return
	}
	id := r.PathValue("id")
	s.cartMu.Lock()
	defer s.cartMu.Unlock()
	ss, err := s.store.Get(id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	if ss.Order != nil {
		writeErr(w, 400, errOrdered.Error())
		return
	}
	lines := s.ucp.CartLines(ss.CartID)
	if len(lines) == 0 {
		writeErr(w, 400, "cart is empty")
		return
	}
	cart := s.ucp.GetCart(ss.CartID)
	if cart.ID == "" {
		writeErr(w, 400, "cart is empty")
		return
	}
	amount := cart.Totals[len(cart.Totals)-1].Amount

	s.pay.mu.Lock()
	defer s.pay.mu.Unlock()
	now := s.ucp.now()
	s.pay.gcLocked(now)
	if len(s.pay.m) >= maxIntents {
		writeErr(w, 503, "too many open payments, try again later")
		return
	}
	if old, ok := s.pay.m[s.pay.bySession[id]]; ok && old.Status == "pending" {
		old.Status = "superseded"
	}
	in := &payIntent{ID: "pay_" + newID(12), SessionID: id, CartID: ss.CartID, Amount: amount, Status: "pending", Expires: now.Add(payIntentTTL)}
	s.pay.m[in.ID] = in
	s.pay.bySession[id] = in.ID

	payURL := s.origin(r) + "/pay/" + in.ID
	writeJSON(w, 200, map[string]any{
		"intent_id":    in.ID,
		"status":       in.Status,
		"amount_minor": in.Amount,
		"amount":       float64(in.Amount) / 100,
		"currency":     s.cat.Currency,
		"expires_at":   in.Expires.UTC().Format(time.RFC3339),
		"pay_url":      payURL,
		"qr_url":       "/api/qr?text=" + url.QueryEscape(payURL),
		"demo":         true,
		"note":         "Demo UPI: not connected to any bank, UPI app or payment gateway.",
	})
}

// handlePayGet (phone and TV) reports the intent state and what is being paid for.
func (s *Server) handlePayGet(w http.ResponseWriter, r *http.Request) {
	s.pay.mu.Lock()
	in, ok := s.pay.m[r.PathValue("id")]
	if !ok {
		s.pay.mu.Unlock()
		writeErr(w, 404, "payment not found or expired")
		return
	}
	status := s.payStatus(in)
	cp := *in
	s.pay.mu.Unlock()
	resp := map[string]any{
		"intent_id": cp.ID, "status": status, "amount": float64(cp.Amount) / 100, "currency": s.cat.Currency,
		"expires_at": cp.Expires.UTC().Format(time.RFC3339), "merchant": "Shop the Moment (demo)", "demo": true,
	}
	if status == "pending" {
		resp["lines"] = s.payLines(s.ucp.CartLines(cp.CartID))
	}
	if status == "paid" {
		if ss, err := s.store.Get(cp.SessionID); err == nil && ss.Order != nil {
			resp["order_id"] = ss.Order.ID
		}
	}
	writeJSON(w, 200, resp)
}

// handlePayDecide (phone) approves or declines. Approval runs the UCP checkout.
func (s *Server) handlePayDecide(w http.ResponseWriter, r *http.Request, approve bool) {
	if !s.ucpLimiter.Allow(clientIP(r)) {
		writeErr(w, 429, "too many requests, slow down")
		return
	}
	s.cartMu.Lock()
	defer s.cartMu.Unlock()
	s.pay.mu.Lock()
	defer s.pay.mu.Unlock()
	in, ok := s.pay.m[r.PathValue("id")]
	if !ok {
		writeErr(w, 404, "payment not found or expired")
		return
	}
	if st := s.payStatus(in); st != "pending" {
		writeJSON(w, 409, map[string]any{"error": "this payment is " + st, "status": st})
		return
	}
	if !approve {
		in.Status = "declined"
		writeJSON(w, 200, map[string]any{"status": in.Status})
		return
	}
	// The cart must still be what was priced when the QR was shown.
	cart := s.ucp.GetCart(in.CartID)
	if cart.ID == "" || cart.Totals[len(cart.Totals)-1].Amount != in.Amount {
		in.Status = "superseded"
		writeJSON(w, 409, map[string]any{"error": "the cart changed after this QR was shown; scan the new one on the TV", "status": in.Status})
		return
	}
	ss, code, err := s.placeOrderLocked(in.SessionID, ucpUPIHandler, ucpUPITokenHead+in.ID, s.origin(r), "upi")
	if err != nil {
		writeErr(w, code, err.Error())
		return
	}
	in.Status = "paid"
	writeJSON(w, 200, map[string]any{"status": in.Status, "order_id": ss.Order.ID})
}
