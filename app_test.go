package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func mustCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCatalogLoadsAndRenders(t *testing.T) {
	c := mustCatalog(t)
	if n := len(c.All()); n < 20 {
		t.Fatalf("want >=20 products, got %d", n)
	}
	for _, p := range c.All() {
		svg := productThumbSVG(p, p.DefaultColor())
		if !strings.Contains(svg, "<svg") || len(svg) < 400 {
			t.Errorf("%s: thumbnail looks empty", p.ID)
		}
		if p.Brand != "" && !strings.Contains(strings.ToUpper(svg), strings.ToUpper(p.Brand)) && p.Category != "smartwatch" {
			t.Errorf("%s: brand wordmark missing from art", p.ID)
		}
	}
}

func TestGroundingExactSimilarAndNone(t *testing.T) {
	c := mustCatalog(t)
	ads := buildAds(c)
	get := func(scene string) []Detection {
		_, sc := findScene(ads, scene)
		return Ground(c, CachedCandidates(c, sc))
	}

	d := get("morning-run-1") // Stride white sneaker with legible brand + Hydra orange bottle
	if d[0].MatchLevel != "exact" || d[0].Exact.Product.ID != "stride-pulse" {
		t.Fatalf("sneaker: want exact stride-pulse, got %+v", d[0].MatchLevel)
	}
	if d[1].MatchLevel != "exact" || d[1].Exact.Product.ID != "hydra-steel" {
		t.Fatalf("bottle: want exact hydra-steel (only orange Hydra), got %s", d[1].MatchLevel)
	}
	for _, s := range d[0].Similar {
		if s.Product.ID == "stride-pulse" || s.Product.Category != "sneakers" {
			t.Errorf("similar list must exclude the exact product and stay in category: %s", s.Product.ID)
		}
	}

	d = get("quiet-hours-2") // wayfarer exact; backpack with hidden brand must NOT be exact
	if d[0].MatchLevel != "exact" || d[0].Exact.Product.ID != "lumo-wayfarer" {
		t.Fatalf("sunglasses: want exact lumo-wayfarer, got %s", d[0].MatchLevel)
	}
	if d[1].MatchLevel != "similar" || d[1].Exact != nil || len(d[1].Similar) == 0 {
		t.Fatalf("backpack without legible brand must be similar-only, got %+v", d[1])
	}

	d = get("quiet-hours-1") // lamp is not sold
	if d[2].MatchLevel != "none" || d[2].InCatalog || len(d[2].Similar) != 0 {
		t.Fatalf("lamp: want no catalog match, got %+v", d[2])
	}
}

func TestGroundingRefusesExactWhenColourMissing(t *testing.T) {
	c := mustCatalog(t)
	// Brand and category match, but Stride sneakers do not come in red.
	d := Ground(c, []Candidate{{Category: "sneakers", BrandText: "Stride", Color: "red", Attributes: []string{"mesh upper"}}})
	if d[0].Exact != nil {
		t.Fatalf("red Stride sneakers do not exist; must not be exact")
	}
}

func TestStoreCartOrderAndFunnel(t *testing.T) {
	st := NewStore()
	ss := st.NewSession("morning-run")
	if _, err := st.SetCart(ss.ID, "cart_x", []CartItem{{ProductID: "stride-pulse", Color: "white", Qty: 1}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.SetOrder(ss.ID, "chk_x", Order{ID: "DEMO-1", Total: 120, ItemCount: 1, Simulated: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Order == nil || got.CheckoutID != "chk_x" || !got.Order.Simulated {
		t.Fatalf("bad order: %+v", got.Order)
	}
	if _, err := st.SetOrder(ss.ID, "chk_y", Order{ID: "DEMO-2"}); err == nil {
		t.Fatal("second order must fail")
	}
	if _, err := st.SetCart(ss.ID, "cart_x", nil); err == nil {
		t.Fatal("cart must be locked after the order")
	}
	for _, n := range []string{"ad_view", "panel_opened", "panel_opened", "add_to_cart"} {
		st.AddEvent(ss.ID, n, nil)
	}
	a := st.Analytics()
	if a.Live.Funnel[0].Sessions != 1 || a.Live.Funnel[1].Sessions != 1 || a.Live.Totals["panel_opened"] != 2 {
		t.Fatalf("funnel should count unique sessions: %+v", a.Live)
	}
	if !a.Simulated.Simulated || !strings.Contains(strings.ToLower(a.Simulated.Label), "simulated") {
		t.Fatal("simulated sample must be labelled")
	}
}

func TestRuleAgent(t *testing.T) {
	c := mustCatalog(t)
	st := NewStore()
	ss := st.NewSession("")
	cc := ChatContext{SessionID: ss.ID, ProductID: "stride-pulse", Color: "white", Category: "sneakers"}

	r := ruleAgent(c, st, cc, "Find a cheaper alternative")
	if len(r.Cards) == 0 || r.Source != "rules" {
		t.Fatalf("cheaper: %+v", r)
	}
	for _, card := range r.Cards {
		if card.Product.Price >= 120 || card.Product.Category != "sneakers" {
			t.Errorf("not cheaper/in-category: %s %.0f", card.Product.ID, card.Product.Price)
		}
	}
	r = ruleAgent(c, st, cc, "Show me this in black")
	if r.Focus == nil || r.Focus.Color != "black" || r.Cards[0].Product.ID != "stride-pulse" {
		t.Fatalf("colour: %+v", r)
	}
	r = ruleAgent(c, st, cc, "do you have this in red?") // pulse has no red -> other red sneakers
	for _, card := range r.Cards {
		if card.Product.ID == "stride-pulse" || !card.Product.HasColor("red") {
			t.Errorf("unexpected red result %s", card.Product.ID)
		}
	}
	r = ruleAgent(c, st, cc, "anything under $70?")
	if len(r.Cards) == 0 {
		t.Fatal("want results under $70")
	}
	for _, card := range r.Cards {
		if card.Product.Price > 70 {
			t.Errorf("over budget: %s", card.Product.ID)
		}
	}
	r = ruleAgent(c, st, cc, "send to phone")
	if !r.Shortlist {
		t.Fatalf("shortlist not applied: %+v", r)
	}
	if got, _ := st.Get(ss.ID); len(got.Selected) != 1 || got.Selected[0].ProductID != "stride-pulse" {
		t.Fatalf("selection missing: %+v", got.Selected)
	}
	r = ruleAgent(c, st, ChatContext{SessionID: ss.ID}, "zzzz qqqq")
	if len(r.Cards) != 0 || r.Reply == "" {
		t.Fatalf("nonsense should yield an honest empty reply: %+v", r)
	}
}

// fakeVertex emulates generateContent for both vision (responseSchema) and
// function calling, including thought-signature echo checks.
func fakeVertex(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthenticated", 401)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		gc, _ := body["generationConfig"].(map[string]any)
		if _, ok := gc["responseSchema"]; ok {
			dets := `{"detections":[{"label":"white sneaker","category":"sneakers","brand_text":"Stride","primary_color":"white","attributes":["mesh upper","orange accent","white midsole"],"evidence":"Stride wordmark on heel","confidence":1.7,"box_2d":[300,100,800,600]},{"label":"mystery","category":"toaster","brand_text":"","primary_color":"chartreuse","attributes":[],"evidence":"","confidence":0.4}]}`
			json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": dets}}}}}})
			return
		}
		contents := body["contents"].([]any)
		last := contents[len(contents)-1].(map[string]any)
		parts := last["parts"].([]any)
		if _, isResp := parts[0].(map[string]any)["functionResponse"]; isResp {
			// the model turn must have been echoed verbatim, signature included
			prev := contents[len(contents)-2].(map[string]any)["parts"].([]any)[0].(map[string]any)
			if prev["thoughtSignature"] != "sig-123" {
				t.Errorf("thought signature was not echoed back")
			}
			json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Here are two options under $100."}}}}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{
			map[string]any{"functionCall": map[string]any{"name": "search_catalog", "args": map[string]any{"category": "sneakers", "max_price": 100}}, "thoughtSignature": "sig-123"},
		}}}}})
	}))
}

func TestGeminiClientAgainstFakeVertex(t *testing.T) {
	t.Setenv("GOOGLE_ACCESS_TOKEN", "test-token")
	fv := fakeVertex(t)
	defer fv.Close()
	g := &Gemini{Project: "p", Location: "global", Model: "m", hc: fv.Client(), tokens: &tokenSource{http: fv.Client()}, override: fv.URL}
	c := mustCatalog(t)

	cands, err := g.AnalyzeFrame(context.Background(), bytes.Repeat([]byte{1}, 100), "image/png")
	if err != nil || len(cands) != 2 {
		t.Fatalf("analyze: %v %v", err, cands)
	}
	if cands[0].Confidence != 1 || cands[0].Color != "white" || cands[0].Category != "sneakers" {
		t.Errorf("candidate not normalised: %+v", cands[0])
	}
	if cands[1].Category != "other" || cands[1].Color != "" {
		t.Errorf("unknown category/colour must be sanitised: %+v", cands[1])
	}
	dets := Ground(c, cands)
	if dets[0].MatchLevel != "exact" || dets[0].Exact.Product.ID != "stride-pulse" {
		t.Errorf("live candidate should ground to stride-pulse, got %s", dets[0].MatchLevel)
	}
	if dets[1].InCatalog {
		t.Error("toaster is not in the catalog")
	}

	st := NewStore()
	ss := st.NewSession("")
	res := RunAgent(context.Background(), g, c, st, ChatContext{SessionID: ss.ID}, nil, "show me sneakers under $100")
	if res.Source != "gemini" || res.Reply == "" || len(res.Trace) != 1 || res.Trace[0].Name != "search_catalog" {
		t.Fatalf("agent result: %+v", res)
	}
	for _, card := range res.Cards {
		if card.Product.Price > 100 || card.Product.Category != "sneakers" {
			t.Errorf("tool results must be catalog-grounded: %s", card.Product.ID)
		}
	}
}

func TestAgentFallsBackWhenGeminiFails(t *testing.T) {
	t.Setenv("GOOGLE_ACCESS_TOKEN", "wrong-token")
	fv := fakeVertex(t)
	defer fv.Close()
	g := &Gemini{Project: "p", Location: "global", Model: "m", hc: fv.Client(), tokens: &tokenSource{http: fv.Client()}, override: fv.URL}
	c := mustCatalog(t)
	st := NewStore()
	ss := st.NewSession("")
	res := RunAgent(context.Background(), g, c, st, ChatContext{SessionID: ss.ID, ProductID: "stride-pulse", Color: "white"}, nil, "cheaper please")
	if res.Source != "rules" || res.FallbackBy == "" || len(res.Cards) == 0 {
		t.Fatalf("expected rules fallback with reason, got %+v", res)
	}
}

// -------------------------------------------------------------- HTTP flow ---

func testServer(t *testing.T, g *Gemini) (*httptest.Server, *Server) {
	t.Helper()
	web := fstest.MapFS{
		"tv.html": {Data: []byte("tv")}, "mobile.html": {Data: []byte("mobile")},
		"dashboard.html": {Data: []byte("dash")}, "eval.html": {Data: []byte("eval")},
		"static/app.css": {Data: []byte("x")},
	}
	s := NewServer(mustCatalog(t), NewStore(), g, web)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return ts, s
}

func call(t *testing.T, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestEndToEndFlow(t *testing.T) {
	ts, _ := testServer(t, nil)

	code, out := call(t, "POST", ts.URL+"/api/sessions", map[string]string{"ad_id": "morning-run"})
	if code != 201 {
		t.Fatalf("new session: %d %v", code, out)
	}
	sid := out["session"].(map[string]any)["id"].(string)
	mobile := out["mobile_url"].(string)
	if !strings.HasSuffix(mobile, "/m/"+sid) || strings.ContainsAny(sid, "0O1lI") {
		t.Fatalf("mobile url / id: %s %s", mobile, sid)
	}

	// Viewer events, then analysis of the paused frame (no live AI -> cached).
	for _, n := range []string{"ad_view", "panel_opened"} {
		if code, _ := call(t, "POST", ts.URL+"/api/events", map[string]any{"session_id": sid, "name": n}); code != 202 {
			t.Fatalf("event %s: %d", n, code)
		}
	}
	if code, _ := call(t, "POST", ts.URL+"/api/events", map[string]any{"session_id": sid, "name": "add_to_cart"}); code != 400 {
		t.Fatalf("browsers must not be able to post add_to_cart, got %d", code)
	}
	code, out = call(t, "POST", ts.URL+"/api/analyze", map[string]any{"session_id": sid, "scene_id": "morning-run-1"})
	if code != 200 || out["source"] != "cached" {
		t.Fatalf("analyze: %d %v", code, out)
	}
	d0 := out["detections"].([]any)[0].(map[string]any)
	if d0["match_level"] != "exact" || d0["exact"].(map[string]any)["product"].(map[string]any)["id"] != "stride-pulse" {
		t.Fatalf("expected exact stride-pulse: %v", d0)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/analyze", map[string]any{"session_id": sid, "scene_id": "morning-run-1", "mode": "live"}); code != 503 {
		t.Fatalf("live mode without Gemini should be 503, got %d", code)
	}

	// Send to phone, phone opens the session, adds to cart, checks out.
	if code, _ = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/select", map[string]string{"product_id": "stride-pulse", "color": "white"}); code != 200 {
		t.Fatalf("select: %d", code)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/select", map[string]string{"product_id": "stride-pulse", "color": "red"}); code != 400 {
		t.Fatalf("unavailable variant must be rejected, got %d", code)
	}
	code, out = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/phone-opened", nil)
	sel := out["selected"].([]any)
	if code != 200 || len(sel) != 1 || sel[0].(map[string]any)["product"].(map[string]any)["id"] != "stride-pulse" {
		t.Fatalf("phone should see the same product: %d %v", code, out)
	}
	call(t, "POST", ts.URL+"/api/sessions/"+sid+"/phone-opened", nil) // idempotent for funnel
	code, out = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/cart", map[string]any{"product_id": "stride-pulse", "color": "white", "delta": 1})
	if code != 200 || out["cart_total"].(float64) != 120 {
		t.Fatalf("cart: %d %v", code, out)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/cart", map[string]any{"product_id": "nope", "delta": 1}); code != 400 {
		t.Fatalf("unknown product: %d", code)
	}
	code, out = call(t, "POST", ts.URL+"/api/sessions/"+sid+"/checkout", nil)
	order := out["session"].(map[string]any)["order"].(map[string]any)
	if code != 200 || order["simulated"] != true || !strings.HasPrefix(order["id"].(string), "DEMO-") {
		t.Fatalf("checkout: %d %v", code, out)
	}

	code, out = call(t, "GET", ts.URL+"/api/analytics", nil)
	live := out["live"].(map[string]any)
	funnel := live["funnel"].([]any)
	want := map[string]float64{"ad_view": 1, "panel_opened": 1, "qr_handoff_opened": 1, "add_to_cart": 1, "demo_purchase_completed": 1}
	for _, st := range funnel {
		m := st.(map[string]any)
		if w, ok := want[m["event"].(string)]; ok && m["sessions"].(float64) != w {
			t.Errorf("funnel %s = %v, want %v", m["event"], m["sessions"], w)
		}
	}
	if live["event_totals"].(map[string]any)["qr_handoff_opened"].(float64) != 1 {
		t.Error("qr_handoff_opened must be recorded once per session")
	}

	// Chat, QR, images, pages, expiry.
	code, out = call(t, "POST", ts.URL+"/api/chat", map[string]any{"session_id": sid, "message": "find a cheaper one", "product_id": "stride-pulse", "color": "white", "category": "sneakers"})
	res := out["result"].(map[string]any)
	if code != 200 || res["source"] != "rules" || len(res["cards"].([]any)) == 0 {
		t.Fatalf("chat: %d %v", code, out)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/chat", map[string]any{"session_id": sid, "message": ""}); code != 400 {
		t.Fatalf("empty chat: %d", code)
	}
	if code, _ = call(t, "GET", ts.URL+"/api/sessions/zzzzzzzz", nil); code != 404 {
		t.Fatalf("unknown session: %d", code)
	}
	for path, ct := range map[string]string{"/api/qr?text=" + urlQueryEscape(mobile): "image/svg+xml", "/img/stride-pulse.svg?color=black": "image/svg+xml", "/scenes/quiet-hours-1.svg": "image/svg+xml", "/": "text/html", "/m/" + sid: "text/html", "/dashboard": "text/html", "/healthz": "application/json"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil || resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), ct) {
			t.Errorf("GET %s: %v %v", path, err, resp)
		}
		if resp != nil {
			if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
				t.Errorf("%s: missing CSP", path)
			}
			resp.Body.Close()
		}
	}
	if resp, _ := http.Get(ts.URL + "/api/qr?text="); resp.StatusCode != 400 {
		t.Errorf("empty qr text should be 400")
	}
}

func TestAnalyzeUsesGeminiThenFallsBack(t *testing.T) {
	t.Setenv("GOOGLE_ACCESS_TOKEN", "test-token")
	fv := fakeVertex(t)
	defer fv.Close()
	g := &Gemini{Project: "p", Location: "global", Model: "fake-model", hc: fv.Client(), tokens: &tokenSource{http: fv.Client()}, override: fv.URL}
	ts, _ := testServer(t, g)

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for x := 0; x < 32; x++ {
		img.Set(x, x, color.RGBA{255, 0, 0, 255})
	}
	png.Encode(&buf, img)
	frame := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	_, ss := call(t, "POST", ts.URL+"/api/sessions", map[string]string{})
	sid := ss["session"].(map[string]any)["id"].(string)
	code, out := call(t, "POST", ts.URL+"/api/analyze", map[string]any{"session_id": sid, "scene_id": "morning-run-1", "frame": frame})
	if code != 200 || out["source"] != "gemini" || out["model"] != "fake-model" {
		t.Fatalf("expected live result: %d %v", code, out)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/analyze", map[string]any{"scene_id": "morning-run-1", "frame": "data:text/html;base64,AAAA"}); code != 400 {
		t.Fatalf("non-image frame must be rejected, got %d", code)
	}

	t.Setenv("GOOGLE_ACCESS_TOKEN", "revoked") // Vertex now answers 401
	g.tokens = &tokenSource{http: fv.Client()}
	code, out = call(t, "POST", ts.URL+"/api/analyze", map[string]any{"scene_id": "morning-run-1", "frame": frame})
	if code != 200 || out["source"] != "cached" || out["fallback_reason"] == "" {
		t.Fatalf("expected cached fallback with reason: %d %v", code, out)
	}
	if code, _ = call(t, "POST", ts.URL+"/api/analyze", map[string]any{"scene_id": "morning-run-1", "frame": frame, "mode": "live"}); code != 502 {
		t.Fatalf("live mode must surface the failure, got %d", code)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(3, 1<<62)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatal("should allow")
		}
	}
	if l.Allow("a") || !l.Allow("b") {
		t.Fatal("limit is per key")
	}
}

// ---------------------------------------------------------------------- QR ---

func TestQRDecodesWithOpenCV(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	if err := exec.Command("python3", "-c", "import cv2").Run(); err != nil {
		t.Skip("OpenCV not available")
	}
	dir := t.TempDir()
	inputs := []string{
		"https://shop-the-moment-abc123-as.a.run.app/m/k3m7q2xw",
		"http://localhost:8080/m/abcd2345",
		"x",
		strings.Repeat("A", 40), // v3
		"https://example.com/m/k3m7q2xw?src=tv&x=" + strings.Repeat("z", 60), // v5-6
		strings.Repeat("0123456789", 12),                                     // 120 bytes -> v8
		strings.Repeat("qrcode-éü-日本", 11),                                   // multibyte, 198 bytes -> v10
	}
	var list []string
	for i, in := range inputs {
		m, err := EncodeQR(in)
		if err != nil {
			t.Fatal(err)
		}
		const scale, quiet = 8, 4
		n := len(m)
		img := image.NewGray(image.Rect(0, 0, (n+2*quiet)*scale, (n+2*quiet)*scale))
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				if m[y][x] {
					for dy := 0; dy < scale; dy++ {
						for dx := 0; dx < scale; dx++ {
							img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{0})
						}
					}
				}
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		p := filepath.Join(dir, "q"+string(rune('a'+i))+".png")
		os.WriteFile(p, buf.Bytes(), 0o644)
		os.WriteFile(p+".txt", []byte(in), 0o644)
		list = append(list, p)
	}
	script := `
import sys, cv2
bad = 0
for p in sys.argv[1:]:
    want = open(p + ".txt", encoding="utf-8").read()
    img = cv2.imread(p)
    got, pts, _ = cv2.QRCodeDetector().detectAndDecode(img)
    ok = got == want
    print(("OK  " if ok else "FAIL"), p.split("/")[-1], len(want), repr(got[:40]))
    bad += 0 if ok else 1
sys.exit(1 if bad else 0)
`
	out, err := exec.Command("python3", append([]string{"-c", script}, list...)...).CombinedOutput()
	t.Log("\n" + string(out))
	if err != nil {
		t.Fatalf("OpenCV could not decode every generated QR code: %v", err)
	}
}

func TestQRRejectsTooLong(t *testing.T) {
	if _, err := EncodeQR(strings.Repeat("a", 300)); err == nil {
		t.Fatal("expected error for oversized input")
	}
	svg, err := QRSVG("https://example.com/m/abcd2345")
	if err != nil || !strings.HasPrefix(svg, "<svg") {
		t.Fatalf("svg: %v", err)
	}
}
