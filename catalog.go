package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

//go:embed data/catalog.json
var catalogJSON []byte

// colorHex maps the small colour vocabulary used by the catalog, the vision
// prompt and the UI swatches.
var colorHex = map[string]string{
	"white":  "#F4F4F2",
	"black":  "#1B1B1F",
	"blue":   "#2F6FED",
	"red":    "#D63A3A",
	"green":  "#2E8B57",
	"orange": "#F28C28",
	"grey":   "#8E9196",
	"navy":   "#1F2A5A",
	"silver": "#C4C8CC",
	"gold":   "#C9A227",
	"pink":   "#E985B0",
	"brown":  "#7A4B2A",
}

// colorAliases normalises words a model or a viewer might use.
var colorAliases = map[string]string{
	"gray": "grey", "tortoise": "brown", "tan": "brown", "beige": "white",
	"cream": "white", "ivory": "white", "off-white": "white", "dark blue": "navy",
	"light blue": "blue", "teal": "green", "olive": "green", "yellow": "gold",
	"rose": "pink", "purple": "blue", "charcoal": "grey", "maroon": "red",
}

var knownCategories = []string{"sneakers", "backpack", "headphones", "smartwatch", "sunglasses", "water_bottle", "jacket", "cap"}

// categoryWords maps free-text words to catalog categories.
var categoryWords = map[string]string{
	"shoe": "sneakers", "shoes": "sneakers", "sneaker": "sneakers", "sneakers": "sneakers", "trainer": "sneakers", "trainers": "sneakers", "runner": "sneakers", "runners": "sneakers",
	"backpack": "backpack", "backpacks": "backpack", "bag": "backpack", "bags": "backpack", "rucksack": "backpack", "daypack": "backpack",
	"headphone": "headphones", "headphones": "headphones", "earbud": "headphones", "earbuds": "headphones", "earphones": "headphones", "headset": "headphones",
	"watch": "smartwatch", "watches": "smartwatch", "smartwatch": "smartwatch", "band": "smartwatch",
	"sunglasses": "sunglasses", "sunglass": "sunglasses", "shades": "sunglasses", "glasses": "sunglasses",
	"bottle": "water_bottle", "bottles": "water_bottle", "flask": "water_bottle",
	"jacket": "jacket", "jackets": "jacket", "coat": "jacket", "fleece": "jacket", "shell": "jacket",
	"cap": "cap", "caps": "cap", "hat": "cap", "hats": "cap", "snapback": "cap",
}

func normColor(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, ok := colorHex[s]; ok {
		return s
	}
	if a, ok := colorAliases[s]; ok {
		return a
	}
	// "light grey", "matte black": try the last word.
	if f := strings.Fields(s); len(f) > 1 {
		return normColor(f[len(f)-1])
	}
	return ""
}

func normCategory(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(strings.ReplaceAll(s, " ", "_"), "-", "_")
	for _, c := range knownCategories {
		if s == c {
			return c
		}
	}
	if c, ok := categoryWords[strings.ReplaceAll(s, "_", "")]; ok {
		return c
	}
	return ""
}

type Variant struct {
	Color   string `json:"color"`
	Hex     string `json:"hex"`
	SKU     string `json:"sku"`
	InStock bool   `json:"in_stock"`
}

type Product struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Brand       string    `json:"brand"`
	Category    string    `json:"category"`
	Price       float64   `json:"price"`
	Currency    string    `json:"currency"`
	Attributes  []string  `json:"attributes"`
	Description string    `json:"description"`
	Variants    []Variant `json:"variants"`
}

type rawProduct struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Brand       string   `json:"brand"`
	Category    string   `json:"category"`
	Price       float64  `json:"price"`
	Colors      []string `json:"colors"`
	OutOfStock  []string `json:"out_of_stock"`
	Attributes  []string `json:"attributes"`
	Description string   `json:"description"`
}

type Catalog struct {
	Currency string
	products []Product
	byID     map[string]*Product
}

func LoadCatalog() (*Catalog, error) {
	var raw struct {
		Currency string       `json:"currency"`
		Products []rawProduct `json:"products"`
	}
	if err := json.Unmarshal(catalogJSON, &raw); err != nil {
		return nil, fmt.Errorf("catalog json: %w", err)
	}
	c := &Catalog{Currency: raw.Currency, byID: map[string]*Product{}}
	for _, r := range raw.Products {
		if r.ID == "" || r.Name == "" || r.Price <= 0 {
			return nil, fmt.Errorf("catalog: invalid product %q", r.ID)
		}
		if _, dup := c.byID[r.ID]; dup {
			return nil, fmt.Errorf("catalog: duplicate id %q", r.ID)
		}
		if normCategory(r.Category) != r.Category {
			return nil, fmt.Errorf("catalog: %s has unknown category %q", r.ID, r.Category)
		}
		p := Product{ID: r.ID, Name: r.Name, Brand: r.Brand, Category: r.Category, Price: r.Price,
			Currency: raw.Currency, Attributes: r.Attributes, Description: r.Description}
		oos := map[string]bool{}
		for _, o := range r.OutOfStock {
			oos[o] = true
		}
		for _, col := range r.Colors {
			hex, ok := colorHex[col]
			if !ok {
				return nil, fmt.Errorf("catalog: %s has unknown colour %q", r.ID, col)
			}
			p.Variants = append(p.Variants, Variant{Color: col, Hex: hex, SKU: strings.ToUpper(r.ID + "-" + col), InStock: !oos[col]})
		}
		if len(p.Variants) == 0 {
			return nil, fmt.Errorf("catalog: %s has no variants", r.ID)
		}
		c.products = append(c.products, p)
	}
	for i := range c.products {
		c.byID[c.products[i].ID] = &c.products[i]
	}
	return c, nil
}

func (c *Catalog) All() []Product { return append([]Product(nil), c.products...) }

func (c *Catalog) Get(id string) (Product, bool) {
	p, ok := c.byID[id]
	if !ok {
		return Product{}, false
	}
	return *p, true
}

// LookupSKU finds the product and variant for a variant SKU such as "STRIDE-PULSE-WHITE".
func (c *Catalog) LookupSKU(sku string) (Product, Variant, bool) {
	sku = strings.ToUpper(strings.TrimSpace(sku))
	for _, p := range c.products {
		for _, v := range p.Variants {
			if v.SKU == sku {
				return p, v, true
			}
		}
	}
	return Product{}, Variant{}, false
}

func (p Product) HasColor(color string) bool {
	for _, v := range p.Variants {
		if v.Color == color && v.InStock {
			return true
		}
	}
	return false
}

func (p Product) HasVariant(color string) bool {
	for _, v := range p.Variants {
		if v.Color == color {
			return true
		}
	}
	return false
}

func (p Product) DefaultColor() string {
	for _, v := range p.Variants {
		if v.InStock {
			return v.Color
		}
	}
	return p.Variants[0].Color
}

// Filter is the single search entry point shared by the tool-calling agent and
// the rule-based fallback, so both produce the same grounded results.
type Filter struct {
	Category  string
	Color     string
	MaxPrice  float64
	MinPrice  float64
	Query     string
	Sort      string // price_asc | price_desc | "" (relevance)
	ExcludeID string
	Limit     int
}

func (c *Catalog) Search(f Filter) []Product {
	type scored struct {
		p Product
		s float64
	}
	var out []scored
	qTokens := tokens(f.Query)
	for _, p := range c.products {
		if f.ExcludeID != "" && p.ID == f.ExcludeID {
			continue
		}
		if f.Category != "" && p.Category != f.Category {
			continue
		}
		if f.Color != "" && !p.HasColor(f.Color) {
			continue
		}
		if f.MaxPrice > 0 && p.Price > f.MaxPrice {
			continue
		}
		if f.MinPrice > 0 && p.Price < f.MinPrice {
			continue
		}
		s := 0.0
		if len(qTokens) > 0 {
			hay := strings.ToLower(p.Name + " " + p.Brand + " " + p.Category + " " + strings.Join(p.Attributes, " ") + " " + p.Description)
			for _, t := range qTokens {
				if strings.Contains(hay, t) {
					s++
				}
			}
			if s == 0 && f.Category == "" {
				continue
			}
		}
		out = append(out, scored{p, s})
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch f.Sort {
		case "price_asc":
			return out[i].p.Price < out[j].p.Price
		case "price_desc":
			return out[i].p.Price > out[j].p.Price
		}
		return out[i].s > out[j].s
	})
	limit := f.Limit
	if limit <= 0 || limit > 8 {
		limit = 4
	}
	res := make([]Product, 0, limit)
	for i := 0; i < len(out) && i < limit; i++ {
		res = append(res, out[i].p)
	}
	return res
}

// Cheaper returns in-category products priced below the reference, closest first.
func (c *Catalog) Cheaper(id string) []Product {
	ref, ok := c.Get(id)
	if !ok {
		return nil
	}
	cands := c.Search(Filter{Category: ref.Category, MaxPrice: ref.Price - 0.01, ExcludeID: id, Limit: 8})
	sort.SliceStable(cands, func(i, j int) bool {
		return math.Abs(ref.Price-cands[i].Price) < math.Abs(ref.Price-cands[j].Price)
	})
	if len(cands) > 3 {
		cands = cands[:3]
	}
	return cands
}

var stop = map[string]bool{"the": true, "a": true, "an": true, "me": true, "show": true, "find": true, "something": true, "with": true, "for": true, "and": true, "in": true, "of": true, "this": true, "that": true, "one": true, "any": true, "some": true, "please": true, "want": true, "need": true, "looking": true, "like": true}

func tokens(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= 3 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}
