package main

import (
	"fmt"
	"sort"
	"strings"
)

// Candidate is what the vision step (Gemini, or the cached fallback) reports
// about a product visible in a frame. It never carries prices or catalog IDs:
// those come only from the catalog.
type Candidate struct {
	Label      string   `json:"label"`
	Category   string   `json:"category"`
	BrandText  string   `json:"brand_text"`
	Color      string   `json:"primary_color"`
	Attributes []string `json:"attributes"`
	Evidence   string   `json:"evidence"`
	Confidence float64  `json:"confidence"`
	Box        []int    `json:"box_2d"` // [ymin, xmin, ymax, xmax], 0-1000
}

type Match struct {
	Product Product  `json:"product"`
	Score   float64  `json:"score"`
	Color   string   `json:"color"` // variant colour to show
	Reasons []string `json:"reasons"`
}

type Detection struct {
	ID         string    `json:"id"`
	Candidate  Candidate `json:"candidate"`
	Exact      *Match    `json:"exact"`
	Similar    []Match   `json:"similar"`
	InCatalog  bool      `json:"category_in_catalog"`
	Note       string    `json:"note"`
	MatchLevel string    `json:"match_level"` // exact | similar | none
}

func normBrand(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// attrWords splits attribute phrases into lower-case words of 4+ letters, so
// fillers such as "low-top" never count as evidence.
func attrWords(list []string) map[string]bool {
	m := map[string]bool{}
	for _, a := range list {
		for _, w := range strings.FieldsFunc(strings.ToLower(a), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) {
			if len(w) >= 4 {
				m[w] = true
			}
		}
	}
	return m
}

type scoredProduct struct {
	p                            Product
	score                        float64
	brandOK, colorOK, categoryOK bool
	reasons                      []string
}

// scoreProduct scores one catalog product against a model candidate. Visible
// features count for more when they are rare within the category: "polarised"
// shared by every sunglasses model says little, "wayfarer" says a lot.
func scoreProduct(p Product, c Candidate, color string, df map[string]int) scoredProduct {
	s := scoredProduct{p: p}
	if p.Category == c.Category {
		s.categoryOK = true
		s.score += 0.35
		s.reasons = append(s.reasons, "same category ("+strings.ReplaceAll(p.Category, "_", " ")+")")
	}
	if cb := normBrand(c.BrandText); cb != "" && cb == normBrand(p.Brand) {
		s.brandOK = true
		s.score += 0.25
		s.reasons = append(s.reasons, fmt.Sprintf("brand %q is legible in the frame", p.Brand))
	}
	if color != "" && p.HasColor(color) {
		s.colorOK = true
		s.score += 0.15
		s.reasons = append(s.reasons, "available in "+color)
	}
	pw := attrWords(p.Attributes)
	weight, matched := 0.0, 0
	for w := range attrWords(c.Attributes) {
		if pw[w] {
			weight += 1 / float64(max(df[w], 1))
		}
	}
	for _, a := range c.Attributes {
		for w := range attrWords([]string{a}) {
			if pw[w] {
				matched++
				break
			}
		}
	}
	if matched > 0 {
		s.score += min(0.25, 0.05*weight)
		s.reasons = append(s.reasons, fmt.Sprintf("%d matching visible feature(s)", matched))
	}
	return s
}

// Ground matches model-reported candidates against the catalog. A product is
// labelled an exact match only when category, legible brand and colour all agree
// and no other product is almost as good; everything else is "similar".
func Ground(cat *Catalog, cands []Candidate) []Detection {
	out := make([]Detection, 0, len(cands))
	for i, c := range cands {
		c.Category = normCategory(c.Category)
		color := normColor(c.Color)
		d := Detection{ID: fmt.Sprintf("d%d", i+1), Candidate: c, MatchLevel: "none"}

		var pool []scoredProduct
		if c.Category != "" {
			var inCat []Product
			df := map[string]int{}
			for _, p := range cat.All() {
				if p.Category == c.Category {
					inCat = append(inCat, p)
					for w := range attrWords(p.Attributes) {
						df[w]++
					}
				}
			}
			for _, p := range inCat {
				pool = append(pool, scoreProduct(p, c, color, df))
			}
		}
		d.InCatalog = len(pool) > 0
		if !d.InCatalog {
			d.Note = "Nothing like it is sold in the demo catalog."
			out = append(out, d)
			continue
		}
		sort.SliceStable(pool, func(i, j int) bool { return pool[i].score > pool[j].score })

		best := pool[0]
		exact := best.categoryOK && best.brandOK && best.colorOK
		ambiguous := false
		if exact && len(pool) > 1 {
			second := pool[1]
			if second.brandOK && second.colorOK && best.score-second.score < 0.08 {
				exact, ambiguous = false, true
			}
		}

		toMatch := func(sp scoredProduct) Match {
			col := color
			if col == "" || !sp.p.HasColor(col) {
				col = sp.p.DefaultColor()
			}
			return Match{Product: sp.p, Score: roundTo(min(sp.score, 0.99), 2), Color: col, Reasons: sp.reasons}
		}

		rest := pool
		if exact {
			m := toMatch(best)
			d.Exact = &m
			d.MatchLevel = "exact"
			rest = pool[1:]
			d.Note = "Category, brand and colour all agree with a catalog product."
		} else {
			d.MatchLevel = "similar"
			if ambiguous {
				d.Note = fmt.Sprintf("The frame doesn't show whether it is %s or %s, so nothing is claimed as exact.", best.p.Name, pool[1].p.Name)
			} else {
				d.Note = "Nothing in the catalog confidently matches, so these are the closest items."
			}
		}
		for _, sp := range rest {
			if len(d.Similar) == 3 {
				break
			}
			d.Similar = append(d.Similar, toMatch(sp))
		}
		out = append(out, d)
	}
	return out
}

func roundTo(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
