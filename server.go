package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	cat        *Catalog
	store      *Store
	gem        *Gemini // nil when live AI is not configured
	ads        []*Ad
	web        fs.FS
	baseURL    string
	limiter    *rateLimiter
	ucp        *UCP
	ucpLimiter *rateLimiter
	cartMu     sync.Mutex // serialises cart read-modify-write per process
}

func NewServer(cat *Catalog, store *Store, gem *Gemini, web fs.FS) *Server {
	return &Server{cat: cat, store: store, gem: gem, ads: buildAds(cat), web: web,
		baseURL: strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		limiter: newRateLimiter(40, time.Minute),
		ucp:     NewUCP(cat), ucpLimiter: newRateLimiter(600, time.Minute)}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": Version})
	})

	s.registerUCP(mux)

	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/ads", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ads": s.ads}) })
	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"currency": s.cat.Currency, "products": s.cat.All()})
	})
	mux.HandleFunc("GET /api/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.cat.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, 404, "unknown product")
			return
		}
		writeJSON(w, 200, p)
	})

	mux.HandleFunc("POST /api/sessions", s.handleNewSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions/{id}/select", s.handleSelect)
	mux.HandleFunc("POST /api/sessions/{id}/unselect", s.handleUnselect)
	mux.HandleFunc("POST /api/sessions/{id}/cart", s.handleCart)
	mux.HandleFunc("POST /api/sessions/{id}/checkout", s.handleCheckout)
	mux.HandleFunc("POST /api/sessions/{id}/phone-opened", s.handlePhoneOpened)

	mux.HandleFunc("POST /api/events", s.handlePostEvent)
	mux.HandleFunc("GET /api/events", s.handleGetEvents)
	mux.HandleFunc("GET /api/analytics", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.store.Analytics()) })

	mux.HandleFunc("POST /api/analyze", s.handleAnalyze)
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("GET /api/qr", s.handleQR)

	mux.HandleFunc("GET /img/{file}", s.handleProductImg)
	mux.HandleFunc("GET /scenes/{file}", s.handleScene)

	static := http.StripPrefix("/static/", http.FileServerFS(mustSub(s.web, "static")))
	mux.Handle("GET /static/", noCache(static))
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(s.web, name)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", page("tv.html"))
	mux.HandleFunc("GET /m/{id}", page("mobile.html"))
	mux.HandleFunc("GET /dashboard", page("dashboard.html"))
	mux.HandleFunc("GET /eval", page("eval.html"))

	return secure(logRequests(mux))
}

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// ---------------------------------------------------------------- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, 413, "request too large")
		} else {
			writeErr(w, 400, "invalid JSON body")
		}
		return false
	}
	return true
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		h.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			return
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.code, time.Since(start).Round(time.Millisecond))
	})
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		return strings.TrimSpace(strings.Split(xf, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimiter is a small fixed-window counter used to protect the paid Gemini
// endpoints from a runaway client.
type rateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*struct {
		n     int
		reset time.Time
	}
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]*struct {
		n     int
		reset time.Time
	}{}}
}

func (l *rateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 5000 {
		for k, v := range l.hits {
			if now.After(v.reset) {
				delete(l.hits, k)
			}
		}
	}
	h := l.hits[key]
	if h == nil || now.After(h.reset) {
		l.hits[key] = &struct {
			n     int
			reset time.Time
		}{1, now.Add(l.window)}
		return true
	}
	h.n++
	return h.n <= l.limit
}

func (s *Server) origin(r *http.Request) string {
	if s.baseURL != "" {
		return s.baseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// ----------------------------------------------------------------- config ---

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := map[string]any{"live_ai": s.gem != nil, "currency": s.cat.Currency, "version": Version}
	if s.gem != nil {
		cfg["model"] = s.gem.Model
	}
	writeJSON(w, 200, cfg)
}

// --------------------------------------------------------------- sessions ---

type SessionView struct {
	Session  Session    `json:"session"`
	Selected []Card     `json:"selected"`
	Cart     []CartLine `json:"cart"`
	Total    float64    `json:"cart_total"`
	Currency string     `json:"currency"`
}

type CartLine struct {
	Product Product `json:"product"`
	Color   string  `json:"color"`
	Qty     int     `json:"qty"`
}

func (s *Server) view(ss Session) SessionView {
	v := SessionView{Session: ss, Selected: []Card{}, Cart: []CartLine{}, Currency: s.cat.Currency}
	for _, sel := range ss.Selected {
		if p, ok := s.cat.Get(sel.ProductID); ok {
			v.Selected = append(v.Selected, Card{Product: p, Color: sel.Color})
		}
	}
	for _, it := range ss.Cart {
		if p, ok := s.cat.Get(it.ProductID); ok {
			v.Cart = append(v.Cart, CartLine{Product: p, Color: it.Color, Qty: it.Qty})
			v.Total += p.Price * float64(it.Qty)
		}
	}
	v.Total = roundTo(v.Total, 2)
	return v
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AdID string `json:"ad_id"`
	}
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	ss := s.store.NewSession(req.AdID)
	mobile := s.origin(r) + "/m/" + ss.ID
	writeJSON(w, 201, map[string]any{"session": ss, "mobile_url": mobile, "qr_url": "/api/qr?text=" + urlQueryEscape(mobile)})
}

func urlQueryEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) (Session, bool) {
	ss, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "session not found or expired")
		return Session{}, false
	}
	return ss, true
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	if ss, ok := s.session(w, r); ok {
		writeJSON(w, 200, s.view(ss))
	}
}

func (s *Server) handlePhoneOpened(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	first, err := s.store.MarkPhoneOpened(id)
	if err != nil {
		writeErr(w, 404, "session not found or expired")
		return
	}
	if first {
		s.store.AddEvent(id, "qr_handoff_opened", nil)
	}
	ss, _ := s.store.Get(id)
	writeJSON(w, 200, s.view(ss))
}

type selectReq struct {
	ProductID string `json:"product_id"`
	Color     string `json:"color"`
}

func (s *Server) checkVariant(w http.ResponseWriter, productID, color string) (Product, string, bool) {
	p, ok := s.cat.Get(productID)
	if !ok {
		writeErr(w, 400, "unknown product")
		return p, "", false
	}
	color = normColor(color)
	if color == "" {
		color = p.DefaultColor()
	}
	if !p.HasColor(color) {
		writeErr(w, 400, "that colour is not available for this product")
		return p, "", false
	}
	return p, color, true
}

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	var req selectReq
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	id := r.PathValue("id")
	p, color, ok := s.checkVariant(w, req.ProductID, req.Color)
	if !ok {
		return
	}
	ss, err := s.store.Select(id, Selection{ProductID: p.ID, Color: color})
	if err != nil {
		code := 400
		if errors.Is(err, errNotFound) {
			code = 404
		}
		writeErr(w, code, err.Error())
		return
	}
	s.store.AddEvent(id, "sent_to_phone", map[string]string{"product_id": p.ID})
	writeJSON(w, 200, s.view(ss))
}

func (s *Server) handleUnselect(w http.ResponseWriter, r *http.Request) {
	var req selectReq
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	ss, err := s.store.Unselect(r.PathValue("id"), req.ProductID)
	if err != nil {
		writeErr(w, 404, "session not found or expired")
		return
	}
	writeJSON(w, 200, s.view(ss))
}

// channelOf reads which screen the shopper used. It is recorded on events only.
func channelOf(v string) string {
	if v == "tv" || v == "phone" {
		return v
	}
	return "unknown"
}

// handleCart changes the shopper's UCP cart by one line and mirrors the result into the session.
func (s *Server) handleCart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProductID string `json:"product_id"`
		Color     string `json:"color"`
		Delta     int    `json:"delta"`
		Channel   string `json:"channel"`
	}
	if !readJSON(w, r, 4<<10, &req) {
		return
	}
	if req.Delta == 0 || req.Delta > 9 || req.Delta < -9 {
		writeErr(w, 400, "delta must be between -9 and 9 and not zero")
		return
	}
	id := r.PathValue("id")
	p, color, ok := s.checkVariant(w, req.ProductID, req.Color)
	if !ok {
		return
	}
	var sku string
	for _, v := range p.Variants {
		if v.Color == color {
			sku = v.SKU
		}
	}

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
	found := false
	for i := range lines {
		if lines[i].SKU == sku {
			found = true
			lines[i].Qty = min(lines[i].Qty+req.Delta, ucpMaxQty)
		}
	}
	if !found && req.Delta > 0 {
		lines = append(lines, ucpStoredLine{SKU: sku, Qty: min(req.Delta, ucpMaxQty)})
	}
	var in ucpCartReq
	for _, l := range lines {
		if l.Qty > 0 {
			in.LineItems = append(in.LineItems, ucpReqLine{Item: ucpReqItem{ID: l.SKU}, Quantity: l.Qty})
		}
	}
	var cart ucpCartResp
	switch {
	case ss.CartID != "" && s.ucp.CartLines(ss.CartID) != nil:
		cart = s.ucp.UpdateCart(ss.CartID, in)
	case len(in.LineItems) > 0:
		cart = s.ucp.CreateCart(in)
	default:
		cart = s.ucp.cartResp(&ucpCart{}) // nothing to create: an empty, id-less cart
		cart.ID = ""
	}
	if cart.UCP.Status == "error" {
		writeErr(w, 400, firstMessage(cart.Messages))
		return
	}
	items := make([]CartItem, 0, len(cart.LineItems))
	for _, l := range cart.LineItems {
		if lp, lv, ok := s.cat.LookupSKU(l.Item.ID); ok {
			items = append(items, CartItem{ProductID: lp.ID, Color: lv.Color, Qty: l.Quantity})
		}
	}
	ss, err = s.store.SetCart(id, cart.ID, items)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Delta > 0 {
		s.store.AddEvent(id, "add_to_cart", map[string]string{"product_id": p.ID, "channel": channelOf(req.Channel)})
	}
	writeJSON(w, 200, s.view(ss))
}

func firstMessage(m []ucpMessage) string {
	if len(m) == 0 {
		return "the merchant refused the request"
	}
	return m[0].Content
}

// handleCheckout runs a UCP checkout for the session's cart with the simulated demo_pay handler.
func (s *Server) handleCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if !readJSON(w, r, 1<<10, &req) {
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
	if len(ss.Cart) == 0 || ss.CartID == "" {
		writeErr(w, 400, "cart is empty")
		return
	}
	base := s.origin(r)
	co := s.ucp.CreateCheckout(ucpCheckoutReq{CartID: ss.CartID}, base)
	if co.ID == "" {
		writeErr(w, 400, firstMessage(co.Messages))
		return
	}
	done := s.ucp.CompleteCheckout(co.ID, ucpCheckoutReq{Payment: &ucpPayment{Instruments: []ucpInstrument{{
		ID: "demo_card_4242", HandlerID: ucpDemoHandler, Type: "demo_token",
		Credential: &ucpCredential{Type: "demo_token", Token: ucpDemoTokenHead + id},
	}}}}, base)
	if done.Status != "completed" || done.Order == nil {
		writeErr(w, 400, firstMessage(done.Messages))
		return
	}
	count := 0
	for _, l := range done.LineItems {
		count += l.Quantity
	}
	total := done.Totals[len(done.Totals)-1].Amount
	ss, err = s.store.SetOrder(id, co.ID, Order{ID: done.Order.ID, Total: float64(total) / 100, Currency: s.cat.Currency, ItemCount: count, Simulated: true})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.store.AddEvent(id, "demo_purchase_completed", map[string]string{"channel": channelOf(req.Channel)})
	writeJSON(w, 200, s.view(ss))
}

// ----------------------------------------------------------------- events ---

func (s *Server) handlePostEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string            `json:"session_id"`
		Name      string            `json:"name"`
		Props     map[string]string `json:"props"`
	}
	if !readJSON(w, r, 8<<10, &req) {
		return
	}
	// The server records the cart, checkout, phone and chat events itself, so
	// browsers may only report these viewing events.
	switch req.Name {
	case "ad_view", "panel_opened", "product_opened", "alternative_selected":
	default:
		writeErr(w, 400, "unsupported event")
		return
	}
	if _, err := s.store.Get(req.SessionID); err != nil {
		writeErr(w, 404, "session not found or expired")
		return
	}
	props := map[string]string{}
	for k, v := range req.Props {
		if len(props) < 4 {
			props[truncate(k, 24)] = truncate(v, 64)
		}
	}
	e := s.store.AddEvent(req.SessionID, req.Name, props)
	writeJSON(w, 202, map[string]int{"id": e.ID})
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	writeJSON(w, 200, map[string]any{"events": s.store.Events(since, 100)})
}

// ---------------------------------------------------------------- analyze ---

type AnalyzeResult struct {
	Source     string      `json:"source"` // gemini | cached
	Model      string      `json:"model,omitempty"`
	LatencyMS  int64       `json:"latency_ms"`
	Fallback   string      `json:"fallback_reason,omitempty"`
	SceneID    string      `json:"scene_id"`
	Detections []Detection `json:"detections"`
}

func decodeFrame(s string) ([]byte, string, error) {
	mime := "image/png"
	if strings.HasPrefix(s, "data:") {
		i := strings.Index(s, ",")
		if i < 0 {
			return nil, "", errors.New("bad data URL")
		}
		head := s[5:i]
		s = s[i+1:]
		switch {
		case strings.HasPrefix(head, "image/png"):
		case strings.HasPrefix(head, "image/jpeg"):
			mime = "image/jpeg"
		default:
			return nil, "", errors.New("frame must be a PNG or JPEG")
		}
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, "", errors.New("frame is not valid base64")
	}
	if len(b) < 64 || len(b) > 4<<20 {
		return nil, "", errors.New("frame size out of range")
	}
	return b, mime, nil
}

func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(clientIP(r)) {
		writeErr(w, 429, "too many requests, slow down")
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
		SceneID   string `json:"scene_id"`
		Frame     string `json:"frame"` // data URL of the paused frame
		Mode      string `json:"mode"`  // "" | cached | live
	}
	if !readJSON(w, r, 6<<20, &req) {
		return
	}
	_, scene := findScene(s.ads, req.SceneID)
	if scene == nil {
		writeErr(w, 400, "unknown scene")
		return
	}
	if req.Mode != "" && req.Mode != "cached" && req.Mode != "live" {
		writeErr(w, 400, "mode must be cached or live")
		return
	}
	if req.Mode == "live" && s.gem == nil {
		writeErr(w, 503, "live AI is not configured on this deployment")
		return
	}

	start := time.Now()
	res := AnalyzeResult{SceneID: scene.ID, Source: "cached"}
	var cands []Candidate
	if s.gem != nil && req.Mode != "cached" && req.Frame != "" {
		img, mime, err := decodeFrame(req.Frame)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		c, err := s.gem.AnalyzeFrame(ctx, img, mime)
		cancel()
		if err == nil && len(c) > 0 {
			cands, res.Source, res.Model = c, "gemini", s.gem.Model
		} else {
			if err == nil {
				err = errors.New("model reported no products")
			}
			log.Printf("analyze: gemini failed, using cached result: %v", err)
			res.Fallback = truncate(err.Error(), 160)
			if req.Mode == "live" {
				writeErr(w, 502, "live analysis failed: "+res.Fallback)
				return
			}
		}
	} else if req.Mode == "live" {
		writeErr(w, 400, "live mode needs a frame")
		return
	}
	if cands == nil {
		cands = CachedCandidates(s.cat, scene)
	}
	res.Detections = Ground(s.cat, cands)
	res.LatencyMS = time.Since(start).Milliseconds()
	writeJSON(w, 200, res)
}

// ------------------------------------------------------------------- chat ---

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(clientIP(r)) {
		writeErr(w, 429, "too many requests, slow down")
		return
	}
	var req struct {
		SessionID string     `json:"session_id"`
		Message   string     `json:"message"`
		ProductID string     `json:"product_id"`
		Color     string     `json:"color"`
		Category  string     `json:"category"`
		History   []ChatTurn `json:"history"`
	}
	if !readJSON(w, r, 32<<10, &req) {
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" || len([]rune(msg)) > 300 {
		writeErr(w, 400, "message must be 1-300 characters")
		return
	}
	if _, err := s.store.Get(req.SessionID); err != nil {
		writeErr(w, 404, "session not found or expired")
		return
	}
	var hist []ChatTurn
	for _, h := range req.History {
		if (h.Role == "user" || h.Role == "assistant") && h.Text != "" {
			hist = append(hist, ChatTurn{Role: h.Role, Text: truncate(h.Text, 400)})
		}
	}
	if len(hist) > 8 {
		hist = hist[len(hist)-8:]
	}
	cc := ChatContext{SessionID: req.SessionID, ProductID: req.ProductID, Color: normColor(req.Color), Category: normCategory(req.Category)}
	start := time.Now()
	res := RunAgent(r.Context(), s.gem, s.cat, s.store, cc, hist, msg)
	s.store.AddEvent(req.SessionID, "chat_message", map[string]string{"source": res.Source})
	writeJSON(w, 200, map[string]any{"result": res, "latency_ms": time.Since(start).Milliseconds()})
}

// ----------------------------------------------------------- QR and images ---

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" || len(text) > 200 {
		writeErr(w, 400, "text must be 1-200 characters")
		return
	}
	svg, err := QRSVG(text)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, svg)
}

func (s *Server) handleProductImg(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("file"), ".svg")
	p, ok := s.cat.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, productThumbSVG(p, normColor(r.URL.Query().Get("color"))))
}

func (s *Server) handleScene(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("file"), ".svg")
	ad, sc := findScene(s.ads, id)
	if sc == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	io.WriteString(w, RenderScene(s.cat, ad, sc))
}
