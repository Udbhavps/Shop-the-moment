package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var ucpAgentRe = regexp.MustCompile(`(?:^|[,\s;])profile="(https?://[^"\s]{1,300})"`)

func ucpProtoErr(w http.ResponseWriter, code int, errCode, content string) {
	writeJSON(w, code, map[string]string{"code": errCode, "content": content})
}

func (u *UCP) idemGet(key, fp string) (ucpIdem, bool, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	e, ok := u.idem[key]
	if !ok || u.now().Sub(e.at) > ucpIdemTTL {
		return ucpIdem{}, false, false
	}
	return e, true, e.fingerprint != fp
}

func (u *UCP) idemPut(key, fp string, status int, body []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.idem[key] = ucpIdem{fingerprint: fp, status: status, body: body, at: u.now()}
}

// ucpRoute wraps an operation with the protocol's header rules.
// UCP-Agent is required on every call. Idempotency-Key is required where idemRequired
// is set (create and complete) and honoured on every other mutating call.
// Request-Id is echoed, or generated when absent (the spec asks callers to send one).
func (s *Server) ucpRoute(idemRequired bool, fn func(r *http.Request, body []byte, base string) (int, any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rid := strings.TrimSpace(r.Header.Get("Request-Id"))
		if rid == "" || len(rid) > 100 {
			rid = "req_" + newID(10)
		}
		w.Header().Set("Request-Id", rid)
		if !s.ucpLimiter.Allow(clientIP(r)) {
			ucpProtoErr(w, 429, "rate_limited", "too many requests, slow down")
			return
		}
		m := ucpAgentRe.FindStringSubmatch(r.Header.Get("UCP-Agent"))
		if m == nil {
			ucpProtoErr(w, 400, "invalid_request", `UCP-Agent header is required, e.g. UCP-Agent: profile="https://platform.example/.well-known/ucp"`)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			ucpProtoErr(w, 413, "invalid_request", "request too large")
			return
		}
		mutating := r.Method != http.MethodGet
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if mutating && idemRequired && key == "" {
			ucpProtoErr(w, 400, "idempotency_key_required", "Idempotency-Key header is required for this operation")
			return
		}
		if len(key) > 200 {
			ucpProtoErr(w, 400, "invalid_request", "Idempotency-Key is too long")
			return
		}
		var fp, ikey string
		if mutating && key != "" {
			fp, ikey = fingerprint(r.Method, r.URL.Path, body), m[1]+"|"+key
			if e, found, conflict := s.ucp.idemGet(ikey, fp); found {
				if conflict {
					ucpProtoErr(w, 409, "idempotency_conflict", "this Idempotency-Key was already used with a different request")
					return
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(e.status)
				w.Write(e.body)
				return
			}
		}
		status, resp := fn(r, body, s.origin(r))
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		if mutating && key != "" {
			s.ucp.idemPut(ikey, fp, status, b)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		w.Write(b)
	}
}

func decodeBody[T any](body []byte) (T, bool) {
	var v T
	if len(strings.TrimSpace(string(body))) == 0 {
		return v, true
	}
	return v, json.Unmarshal(body, &v) == nil
}

func badJSON() (int, any) {
	return 400, map[string]string{"code": "invalid_json", "content": "request body is not valid JSON for this operation"}
}

func (s *Server) registerUCP(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/ucp", s.ucpProfile)
	mux.HandleFunc("GET /ucp/handlers/"+ucpDemoHandler, s.ucpHandlerDoc)
	mux.HandleFunc("GET /order/{id}", s.orderPage)

	created := func(id string, ok bool) int {
		if ok && id != "" {
			return 201
		}
		return 200
	}
	mux.HandleFunc("POST /ucp/v1/carts", s.ucpRoute(true, func(r *http.Request, b []byte, base string) (int, any) {
		in, ok := decodeBody[ucpCartReq](b)
		if !ok {
			return badJSON()
		}
		c := s.ucp.CreateCart(in)
		return created(c.ID, true), c
	}))
	mux.HandleFunc("GET /ucp/v1/carts/{id}", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		return 200, s.ucp.GetCart(r.PathValue("id"))
	}))
	mux.HandleFunc("PUT /ucp/v1/carts/{id}", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		in, ok := decodeBody[ucpCartReq](b)
		if !ok {
			return badJSON()
		}
		return 200, s.ucp.UpdateCart(r.PathValue("id"), in)
	}))
	mux.HandleFunc("POST /ucp/v1/carts/{id}/cancel", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		return 200, s.ucp.CancelCart(r.PathValue("id"))
	}))

	mux.HandleFunc("POST /ucp/v1/checkout-sessions", s.ucpRoute(true, func(r *http.Request, b []byte, base string) (int, any) {
		in, ok := decodeBody[ucpCheckoutReq](b)
		if !ok {
			return badJSON()
		}
		c := s.ucp.CreateCheckout(in, base)
		return created(c.ID, true), c
	}))
	mux.HandleFunc("GET /ucp/v1/checkout-sessions/{id}", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		return 200, s.ucp.GetCheckout(r.PathValue("id"), base)
	}))
	mux.HandleFunc("PUT /ucp/v1/checkout-sessions/{id}", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		in, ok := decodeBody[ucpCheckoutReq](b)
		if !ok {
			return badJSON()
		}
		return 200, s.ucp.UpdateCheckout(r.PathValue("id"), in, base)
	}))
	mux.HandleFunc("POST /ucp/v1/checkout-sessions/{id}/complete", s.ucpRoute(true, func(r *http.Request, b []byte, base string) (int, any) {
		in, ok := decodeBody[ucpCheckoutReq](b)
		if !ok {
			return badJSON()
		}
		return 200, s.ucp.CompleteCheckout(r.PathValue("id"), in, base)
	}))
	mux.HandleFunc("POST /ucp/v1/checkout-sessions/{id}/cancel", s.ucpRoute(false, func(r *http.Request, b []byte, base string) (int, any) {
		return 200, s.ucp.CancelCheckout(r.PathValue("id"), base)
	}))
}

// ucpProfile is the business profile every platform discovers first.
func (s *Server) ucpProfile(w http.ResponseWriter, r *http.Request) {
	base := s.origin(r)
	spec := "https://ucp.dev/" + ucpVersion + "/specification/"
	profile := map[string]any{
		"ucp": map[string]any{
			"version": ucpVersion,
			"services": map[string]any{
				"dev.ucp.shopping": []map[string]any{{
					"version":   ucpVersion,
					"spec":      spec + "overview",
					"transport": "rest",
					"endpoint":  base + "/ucp/v1",
				}},
			},
			"capabilities": map[string]any{
				ucpCapCheckout: []map[string]any{{"version": ucpVersion, "spec": spec + "checkout"}},
				ucpCapCart:     []map[string]any{{"version": ucpVersion, "spec": spec + "cart"}},
			},
			"payment_handlers": map[string]any{
				ucpHandlerName: []map[string]any{{
					"id": ucpDemoHandler, "version": ucpVersion,
					"spec":                  base + "/ucp/handlers/" + ucpDemoHandler,
					"available_instruments": []map[string]any{{"type": "demo_token"}},
				}},
			},
		},
		"keys": []any{}, // request signing is not implemented in this demo
		"demo": map[string]any{
			"note": "Demo merchant for a hackathon prototype. Payments are simulated and no money moves. Not an official Google or UCP deployment.",
		},
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, profile)
}

func (s *Server) ucpHandlerDoc(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name": ucpHandlerName, "id": ucpDemoHandler, "version": ucpVersion,
		"description": "Simulated payment handler for the Shop the Moment demo. No money moves anywhere.",
		"complete_checkout": map[string]any{
			"instrument": map[string]any{"handler_id": ucpDemoHandler, "type": "demo_token"},
			"credential": map[string]string{"type": "demo_token", "token": ucpDemoTokenHead + "<anything>"},
			"rule":       "Any token that starts with " + ucpDemoTokenHead + " is accepted. Other tokens are refused.",
		},
	})
}

func (s *Server) orderPage(w http.ResponseWriter, r *http.Request) {
	o, ok := s.ucp.Order(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var items strings.Builder
	for _, t := range o.Titles {
		fmt.Fprintf(&items, "<li>%s</li>", html.EscapeString(t))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Demo order %s</title>
<style>body{font:18px/1.45 system-ui,sans-serif;max-width:560px;margin:40px auto;padding:0 18px;color:#0e1118}h1{font-size:28px}.note{background:#fff3d6;padding:12px 14px;border-radius:12px}</style></head>
<body><h1>Demo order %s</h1><p class="note">This is a simulation. No payment was taken and nothing will ship.</p><ul>%s</ul><p><strong>Total $%d.%02d</strong> (no tax or shipping modelled)</p><p>Placed %s</p></body></html>`,
		html.EscapeString(o.ID), html.EscapeString(o.ID), items.String(), o.Total/100, o.Total%100, o.PlacedAt.UTC().Format(time.RFC1123))
}
