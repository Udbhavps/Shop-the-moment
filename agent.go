package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ChatTurn struct {
	Role string `json:"role"` // user | assistant
	Text string `json:"text"`
}

// ChatContext tells the agent what the viewer is looking at so that "this",
// "it" and "a cheaper one" resolve to a concrete catalog product.
type ChatContext struct {
	SessionID string `json:"session_id"`
	ProductID string `json:"product_id"`
	Color     string `json:"color"`
	Category  string `json:"category"`
}

type Card struct {
	Product Product `json:"product"`
	Color   string  `json:"color"`
}

type Focus struct {
	ProductID string `json:"product_id"`
	Color     string `json:"color"`
}

type AgentResult struct {
	Reply      string     `json:"reply"`
	Cards      []Card     `json:"cards"`
	Focus      *Focus     `json:"focus,omitempty"`
	Shortlist  bool       `json:"shortlist_changed"`
	Source     string     `json:"source"` // gemini | rules
	Trace      []ToolCall `json:"trace"`
	FallbackBy string     `json:"fallback_reason,omitempty"`
}

// toolbox is the single implementation of the agent's tools. Both the Gemini
// function-calling loop and the rule-based fallback go through it, so the
// products, prices and colours they can mention are always catalog-backed.
type toolbox struct {
	cat       *Catalog
	store     *Store
	ctx       ChatContext
	cards     []Card
	focus     *Focus
	shortlist bool
}

func productView(p Product) map[string]any {
	colors := []string{}
	for _, v := range p.Variants {
		if v.InStock {
			colors = append(colors, v.Color)
		}
	}
	return map[string]any{"id": p.ID, "name": p.Name, "brand": p.Brand, "category": p.Category, "price": p.Price, "currency": p.Currency, "colors_in_stock": colors, "features": p.Attributes}
}

func (t *toolbox) setCards(ps []Product, color string) {
	t.cards = nil
	for _, p := range ps {
		c := color
		if c == "" || !p.HasColor(c) {
			c = p.DefaultColor()
		}
		t.cards = append(t.cards, Card{Product: p, Color: c})
	}
}

func argStr(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argNum(a map[string]any, k string) float64 {
	switch v := a[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

func (t *toolbox) Exec(name string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	switch name {
	case "search_catalog":
		f := Filter{
			Category: normCategory(argStr(args, "category")),
			Color:    normColor(argStr(args, "color")),
			MaxPrice: argNum(args, "max_price"),
			MinPrice: argNum(args, "min_price"),
			Query:    argStr(args, "query"),
			Sort:     argStr(args, "sort"),
			Limit:    4,
		}
		if f.Category == "" && t.ctx.Category != "" && f.Query == "" {
			f.Category = t.ctx.Category // "show me something under $50" while looking at shoes
		}
		res := t.cat.Search(f)
		t.setCards(res, f.Color)
		views := make([]any, 0, len(res))
		for _, p := range res {
			views = append(views, productView(p))
		}
		return map[string]any{"count": len(res), "products": views}, nil
	case "get_product":
		p, ok := t.cat.Get(argStr(args, "product_id"))
		if !ok {
			return nil, fmt.Errorf("unknown product_id")
		}
		t.setCards([]Product{p}, t.ctx.Color)
		return productView(p), nil
	case "find_cheaper_alternatives":
		ref, ok := t.cat.Get(argStr(args, "product_id"))
		if !ok {
			return nil, fmt.Errorf("unknown product_id")
		}
		res := t.cat.Cheaper(ref.ID)
		t.setCards(res, t.ctx.Color)
		views := make([]any, 0, len(res))
		for _, p := range res {
			views = append(views, productView(p))
		}
		return map[string]any{"reference": productView(ref), "count": len(res), "products": views}, nil
	case "shortlist_product":
		p, ok := t.cat.Get(argStr(args, "product_id"))
		if !ok {
			return nil, fmt.Errorf("unknown product_id")
		}
		color := normColor(argStr(args, "color"))
		if color == "" || !p.HasColor(color) {
			color = p.DefaultColor()
		}
		if t.ctx.SessionID == "" {
			return nil, fmt.Errorf("no session")
		}
		if _, err := t.store.Select(t.ctx.SessionID, Selection{ProductID: p.ID, Color: color}); err != nil {
			return nil, err
		}
		t.shortlist = true
		t.focus = &Focus{ProductID: p.ID, Color: color}
		return map[string]any{"saved": true, "product": productView(p), "color": color}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func chatSystemPrompt(cat *Catalog, ctx ChatContext) string {
	focus := "The viewer has not opened a specific product yet."
	if p, ok := cat.Get(ctx.ProductID); ok {
		focus = fmt.Sprintf("The viewer is looking at %s (id %s, %s, %.0f %s, colour %s). \"This\" or \"it\" means this product.", p.Name, p.ID, strings.ReplaceAll(p.Category, "_", " "), p.Price, p.Currency, ctx.Color)
	}
	return `You are the shopping assistant inside a TV shopping panel. Keep replies to 1-2 short sentences; the product cards are shown separately.
Rules:
- Use the tools for every product, price, colour or availability fact. Never state a price, colour or product you did not get from a tool result.
- If the tools return nothing that fits, say so plainly and suggest loosening one filter.
- Never claim an item is the exact product from the ad; the panel already labels exact versus similar matches.
- Do not discuss anything unrelated to shopping for these products.
` + focus
}

// RunAgent answers a viewer message. It prefers Gemini function calling and
// falls back to the rule-based agent if Gemini is unavailable or errors.
func RunAgent(ctx context.Context, g *Gemini, cat *Catalog, store *Store, cc ChatContext, history []ChatTurn, msg string) AgentResult {
	var fallbackReason string
	if g != nil {
		tb := &toolbox{cat: cat, store: store, ctx: cc}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		reply, trace, err := g.Chat(cctx, chatSystemPrompt(cat, cc), history, msg, tb.Exec)
		cancel()
		if err == nil {
			return AgentResult{Reply: reply, Cards: tb.cards, Focus: tb.focus, Shortlist: tb.shortlist, Source: "gemini", Trace: trace}
		}
		fallbackReason = truncate(err.Error(), 160)
	}
	res := ruleAgent(cat, store, cc, msg)
	res.FallbackBy = fallbackReason
	return res
}

var (
	rePriceMax  = regexp.MustCompile(`(?:under|below|less than|max(?:imum)?|within|up to|cheaper than|budget of)\s*(?:\$|rs\.?|₹|usd|inr)?\s*(\d[\d,]*)`)
	rePriceBare = regexp.MustCompile(`(?:\$|₹)\s*(\d[\d,]*)`)
)

func ruleAgent(cat *Catalog, store *Store, cc ChatContext, msg string) AgentResult {
	tb := &toolbox{cat: cat, store: store, ctx: cc}
	low := strings.ToLower(msg)
	res := AgentResult{Source: "rules"}
	cur, haveCur := cat.Get(cc.ProductID)

	// Colour mentioned?
	color := ""
	for _, w := range strings.Fields(strings.NewReplacer(",", " ", ".", " ", "?", " ", "!", " ").Replace(low)) {
		if c := normColor(w); c != "" {
			color = c
			break
		}
	}
	// Category mentioned?
	category := ""
	for _, w := range tokensKeepShort(low) {
		if c, ok := categoryWords[w]; ok {
			category = c
			break
		}
	}
	maxPrice := 0.0
	if m := rePriceMax.FindStringSubmatch(low); m != nil {
		fmt.Sscanf(strings.ReplaceAll(m[1], ",", ""), "%f", &maxPrice)
	} else if m := rePriceBare.FindStringSubmatch(low); m != nil {
		fmt.Sscanf(strings.ReplaceAll(m[1], ",", ""), "%f", &maxPrice)
	}

	call := func(name string, args map[string]any) (any, bool) {
		out, err := tb.Exec(name, args)
		tc := ToolCall{Name: name, Args: args, Result: out}
		if err != nil {
			tc.Error = err.Error()
		}
		res.Trace = append(res.Trace, tc)
		return out, err == nil
	}
	finish := func(reply string) AgentResult {
		res.Reply, res.Cards, res.Focus, res.Shortlist = reply, tb.cards, tb.focus, tb.shortlist
		return res
	}

	wantsCheaper := strings.Contains(low, "cheaper") || strings.Contains(low, "cheap") || strings.Contains(low, "budget") || strings.Contains(low, "less expensive") || strings.Contains(low, "lower price") || strings.Contains(low, "affordable")
	wantsSave := strings.Contains(low, "send to phone") || strings.Contains(low, "save this") || strings.Contains(low, "save it") || strings.Contains(low, "shortlist")

	switch {
	case wantsSave && haveCur:
		call("shortlist_product", map[string]any{"product_id": cur.ID, "color": firstNonEmpty(color, cc.Color)})
		if !tb.shortlist {
			return finish("I couldn't save that right now. Try the Send to phone button.")
		}
		return finish(fmt.Sprintf("Saved %s to your phone shortlist.", cur.Name))

	case wantsCheaper && maxPrice == 0 && haveCur:
		call("find_cheaper_alternatives", map[string]any{"product_id": cur.ID})
		if len(tb.cards) == 0 {
			return finish(fmt.Sprintf("%s is already the lowest-priced %s in the demo catalog.", cur.Name, strings.ReplaceAll(cur.Category, "_", " ")))
		}
		return finish(fmt.Sprintf("Here are cheaper options than %s (%s).", cur.Name, money(cur.Price, cur.Currency)))

	case color != "" && haveCur && (category == "" || category == cur.Category) && maxPrice == 0:
		// "Show me this in black"
		if cur.HasColor(color) {
			tb.setCards([]Product{cur}, color)
			tb.focus = &Focus{ProductID: cur.ID, Color: color}
			return finish(fmt.Sprintf("%s comes in %s.", cur.Name, color))
		}
		call("search_catalog", map[string]any{"category": cur.Category, "color": color})
		if len(tb.cards) == 0 {
			return finish(fmt.Sprintf("Nothing in %s is available in that category right now.", color))
		}
		return finish(fmt.Sprintf("%s isn't offered in %s, but these %s %s are.", cur.Name, color, color, strings.ReplaceAll(cur.Category, "_", " ")))
	}

	// General search.
	args := map[string]any{}
	if category != "" {
		args["category"] = category
	} else if haveCur {
		args["category"] = cur.Category
	}
	if color != "" {
		args["color"] = color
	}
	if maxPrice > 0 {
		args["max_price"] = maxPrice
	}
	if wantsCheaper {
		args["sort"] = "price_asc"
	}
	if category == "" && !haveCur {
		args["query"] = msg
	}
	call("search_catalog", args)
	if len(tb.cards) == 0 {
		return finish("I couldn't find a match for that in the demo catalog. Try a different colour or a higher budget.")
	}
	parts := []string{fmt.Sprintf("Found %d option", len(tb.cards))}
	if len(tb.cards) != 1 {
		parts[0] += "s"
	}
	if color != "" {
		parts = append(parts, "in "+color)
	}
	if maxPrice > 0 {
		parts = append(parts, fmt.Sprintf("under %s", money(maxPrice, cat.Currency)))
	}
	return finish(strings.Join(parts, " ") + ".")
}

// groupIndian formats 1234567 as 12,34,567.
func groupIndian(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	head, tail := s[:len(s)-3], s[len(s)-3:]
	var parts []string
	for len(head) > 2 {
		parts = append([]string{head[len(head)-2:]}, parts...)
		head = head[:len(head)-2]
	}
	if head != "" {
		parts = append([]string{head}, parts...)
	}
	return strings.Join(parts, ",") + "," + tail
}

func tokensKeepShort(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !(r >= 'a' && r <= 'z') })
}

func money(v float64, cur string) string {
	switch cur {
	case "INR":
		return "₹" + groupIndian(int64(v+0.5))
	case "USD", "":
		return fmt.Sprintf("$%.0f", v)
	}
	return fmt.Sprintf("%.0f %s", v, cur)
}
