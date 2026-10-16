package main

import (
	"fmt"
	"strings"
)

// The three sample "ads" are team-made vector scenes (two moments each) so the
// prototype does not depend on third-party video. Each scene also carries a
// cached vision result, used when live Gemini analysis is unavailable. Cached
// results are always reported to the UI as "cached", never as live AI.

const (
	sceneW = 1280
	sceneH = 720
)

type Placement struct {
	ProductID string // catalog product, or "" for something not in the catalog
	Category  string // used when ProductID is ""
	Brand     string // wordmark shown on the item
	Color     string
	X, Y, W   int
	HideBrand bool // draw the item without a legible brand
	Label     string
	// Story layer (shows only): who wears or owns the item and why it matters.
	// In production this would come from the studio's wardrobe and props
	// metadata, keyed by timestamp. A vision model cannot know it.
	Character string
	Role      string // "worn" | "prop"
	Moment    string
}

// Story is shown with a detection when the scene's metadata knows the item.
type Story struct {
	Kind      string `json:"kind"` // worn | prop
	Character string `json:"character"`
	Moment    string `json:"moment"`
	Show      string `json:"show"`
	Episode   string `json:"episode"`
}

type Expected struct {
	Category  string `json:"category"`
	ProductID string `json:"product_id"` // "" = no exact match expected
}

type Scene struct {
	ID       string      `json:"id"`
	AdID     string      `json:"ad_id"`
	Title    string      `json:"title"`
	Theme    [2]string   `json:"-"`
	Items    []Placement `json:"-"`
	Expected []Expected  `json:"expected"`
	SVGURL   string      `json:"svg_url"`
	VideoURL string      `json:"video_url"` // clip without extension; the TV page picks .webm or .mp4
}

type Ad struct {
	Kind    string   `json:"kind"` // "ad" | "show"
	Episode string   `json:"episode,omitempty"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Brand   string   `json:"brand"`
	Tagline string   `json:"tagline"`
	Seconds int      `json:"seconds_per_scene"`
	Scenes  []*Scene `json:"scenes"`
}

func buildAds(cat *Catalog) []*Ad {
	ads := []*Ad{
		{ID: "morning-run", Title: "Morning Run", Brand: "Stride", Tagline: "Find your pace.", Seconds: 7, Scenes: []*Scene{
			{ID: "morning-run-1", Title: "First light", Theme: [2]string{"#ffb98a", "#5aa9a3"}, Items: []Placement{
				{ProductID: "stride-pulse", Color: "white", X: 120, Y: 170, W: 540},
				{ProductID: "hydra-steel", Color: "orange", X: 760, Y: 150, W: 360},
			}},
			{ID: "morning-run-2", Title: "Cool down", Theme: [2]string{"#f6a77b", "#3f7f93"}, Items: []Placement{
				{ProductID: "korvo-pulse", Color: "red", X: 130, Y: 140, W: 460},
				{ProductID: "stride-cap", Color: "black", X: 720, Y: 210, W: 400},
			}},
		}},
		{ID: "weekend-escape", Title: "Weekend Escape", Brand: "Arcline", Tagline: "Pack light. Go far.", Seconds: 7, Scenes: []*Scene{
			{ID: "weekend-escape-1", Title: "Trailhead", Theme: [2]string{"#9fd1a5", "#2d5f4b"}, Items: []Placement{
				{ProductID: "arcline-trail", Color: "green", X: 110, Y: 110, W: 520},
				{ProductID: "lumo-aviator", Color: "gold", X: 720, Y: 220, W: 400},
			}},
			{ID: "weekend-escape-2", Title: "Summit wind", Theme: [2]string{"#a8c8e8", "#3a5f86"}, Items: []Placement{
				{ProductID: "northpeak-shell", Color: "orange", X: 120, Y: 110, W: 520},
				{ProductID: "hydra-lite", Color: "blue", X: 780, Y: 150, W: 340},
			}},
		}},
		{ID: "quiet-hours", Title: "Quiet Hours", Brand: "Korvo", Tagline: "Hear only what matters.", Seconds: 7, Scenes: []*Scene{
			{ID: "quiet-hours-1", Title: "After dark", Theme: [2]string{"#5b4f8f", "#1c1a3a"}, Items: []Placement{
				{ProductID: "korvo-halo", Color: "black", X: 90, Y: 150, W: 470},
				{ProductID: "tempo-one", Color: "black", X: 600, Y: 300, W: 320},
				{Category: "lamp", Brand: "Hearth", Color: "gold", X: 930, Y: 90, W: 300, Label: "table lamp"},
			}},
			{ID: "quiet-hours-2", Title: "Weekend off", Theme: [2]string{"#7f78b8", "#2a2650"}, Items: []Placement{
				{ProductID: "lumo-wayfarer", Color: "black", X: 110, Y: 250, W: 400},
				{ProductID: "arcline-city", Color: "black", X: 640, Y: 110, W: 470, HideBrand: true},
			}},
		}},
		{ID: "neon-harbor", Kind: "show", Title: "Neon Harbor", Episode: "S1 E3 · The Tide Ledger", Seconds: 7, Scenes: []*Scene{
			{ID: "neon-harbor-1", Title: "Dockside, 2 a.m.", Theme: [2]string{"#2c5a78", "#0b1426"}, Items: []Placement{
				{ProductID: "northpeak-shell", Color: "black", X: 90, Y: 120, W: 500, Character: "Mira Vale", Role: "worn", Moment: "Mira wears it through the whole harbor chase."},
				{ProductID: "tempo-one", Color: "black", X: 640, Y: 300, W: 300, Character: "Mira Vale", Role: "worn", Moment: "She checks it at 2:04, the minute the tide ledger goes missing."},
				{ProductID: "tidewell-compass", Color: "gold", X: 960, Y: 150, W: 280, Character: "Mira Vale", Role: "prop", Moment: "The Harbor Compass: her father's pendant and the clue the whole season turns on."},
			}},
			{ID: "neon-harbor-2", Title: "The ledger room", Theme: [2]string{"#7a3f73", "#140d22"}, Items: []Placement{
				{ProductID: "arcline-city", Color: "grey", X: 100, Y: 110, W: 470, Character: "Joel Ashby", Role: "worn", Moment: "Joel carries the stolen ledger in this pack."},
				{ProductID: "dash-snapback", Color: "navy", X: 640, Y: 240, W: 330, HideBrand: true, Character: "Joel Ashby", Role: "worn", Moment: "Pulled low to hide from the dock cameras."},
				{ProductID: "tidewell-compass", Color: "gold", X: 1000, Y: 300, W: 240, Character: "Mira Vale", Role: "prop", Moment: "Left behind in the ledger room, proof that Joel was never alone."},
			}},
		}},
	}
	for _, ad := range ads {
		if ad.Kind == "" {
			ad.Kind = "ad"
		}
		for _, sc := range ad.Scenes {
			sc.AdID = ad.ID
			sc.SVGURL = "/scenes/" + sc.ID + ".svg"
			sc.VideoURL = "/video/" + sc.ID
			for _, it := range sc.Items {
				exp := Expected{Category: it.Category}
				if it.ProductID != "" {
					if p, ok := cat.Get(it.ProductID); ok {
						exp.Category = p.Category
						if !it.HideBrand {
							exp.ProductID = p.ID
						}
					}
				}
				if exp.Category == "lamp" {
					exp.Category = "other"
				}
				sc.Expected = append(sc.Expected, exp)
			}
		}
	}
	return ads
}

func findScene(ads []*Ad, id string) (*Ad, *Scene) {
	for _, ad := range ads {
		for _, sc := range ad.Scenes {
			if sc.ID == id {
				return ad, sc
			}
		}
	}
	return nil, nil
}

func (it Placement) category(cat *Catalog) (category, brand, label string) {
	if it.ProductID == "" {
		return it.Category, it.Brand, it.Label
	}
	p, _ := cat.Get(it.ProductID)
	return p.Category, p.Brand, ""
}

// RenderScene draws one ad moment as a 1280x720 SVG.
func RenderScene(cat *Catalog, ad *Ad, sc *Scene) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">`, sceneW, sceneH, sceneW, sceneH)
	fmt.Fprintf(&b, `<defs><linearGradient id="sky" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`+
		`<linearGradient id="floor" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#000" stop-opacity=".18"/><stop offset="1" stop-color="#000" stop-opacity=".42"/></linearGradient></defs>`, sc.Theme[0], sc.Theme[1])
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="url(#sky)"/>`, sceneW, sceneH)
	fmt.Fprintf(&b, `<circle cx="1060" cy="150" r="90" fill="#fff" opacity=".18"/><circle cx="1060" cy="150" r="52" fill="#fff" opacity=".22"/>`)
	fmt.Fprintf(&b, `<path d="M0 470 Q220 380 460 450 T920 430 T1280 400 L1280 720 L0 720 Z" fill="#000" opacity=".16"/>`)
	fmt.Fprintf(&b, `<rect y="560" width="%d" height="160" fill="url(#floor)"/>`, sceneW)
	for _, it := range sc.Items {
		category, brand, _ := it.category(cat)
		word := brand
		if it.HideBrand {
			word = ""
		}
		color := it.Color
		fmt.Fprintf(&b, `<ellipse cx="%d" cy="%d" rx="%d" ry="13" fill="#000" opacity=".26"/>`, it.X+it.W/2, it.Y+it.W*groundY(category)/200+6, it.W*34/100)
		id := it.ProductID
		fmt.Fprintf(&b, `<svg x="%d" y="%d" width="%d" height="%d" viewBox="0 0 200 200">%s</svg>`, it.X, it.Y, it.W, it.W, drawProduct(category, id, color, word))
	}
	// lower third
	fmt.Fprintf(&b, `<rect x="0" y="628" width="%d" height="92" fill="#0b0d12" opacity=".62"/>`, sceneW)
	fmt.Fprintf(&b, `<text x="56" y="676" font-family="Helvetica, Arial, sans-serif" font-size="34" font-weight="800" fill="#fff">%s</text>`, xmlEscape(ad.Title))
	sub, right := ad.Tagline, ad.Brand
	if ad.Kind == "show" {
		sub, right = ad.Episode, sc.Title
	}
	fmt.Fprintf(&b, `<text x="56" y="705" font-family="Helvetica, Arial, sans-serif" font-size="20" fill="#d5d8e0">%s</text>`, xmlEscape(sub))
	fmt.Fprintf(&b, `<text x="%d" y="684" font-family="Helvetica, Arial, sans-serif" font-size="22" font-weight="800" letter-spacing="3" text-anchor="end" fill="#fff" opacity=".85">%s</text>`, sceneW-56, strings.ToUpper(xmlEscape(right)))
	b.WriteString(`</svg>`)
	return b.String()
}

// CachedCandidates is the stand-in for live vision: what a careful model would
// report for the scene. It feeds the same grounding code as live results.
func CachedCandidates(cat *Catalog, sc *Scene) []Candidate {
	var out []Candidate
	for _, it := range sc.Items {
		category, brand, label := it.category(cat)
		c := Candidate{Category: category, Color: it.Color, Confidence: 0.9}
		if !it.HideBrand {
			c.BrandText = brand
		}
		if it.ProductID != "" {
			p, _ := cat.Get(it.ProductID)
			c.Label = it.Color + " " + strings.ReplaceAll(category, "_", " ")
			c.Attributes = p.Attributes
			if len(c.Attributes) > 4 {
				c.Attributes = c.Attributes[:4]
			}
			if c.BrandText != "" {
				c.Evidence = fmt.Sprintf("%s wordmark is legible on the %s.", c.BrandText, strings.ReplaceAll(category, "_", " "))
			} else {
				c.Evidence = "No brand name or logo is legible on this item."
				c.Confidence = 0.7
			}
		} else {
			c.Label = label
			c.Category = "other" // same value live results are sanitised to
			c.Evidence = "A lamp with a visible wordmark; not a category the demo catalog sells."
			c.Attributes = []string{"fabric shade", "slim stem"}
		}
		// tight bounding box from the placement and the drawing's extent
		bx0, by0, bx1, by1 := artBounds(category)
		pad := 5
		px := func(v int) int { return it.X + it.W*v/200 }
		py := func(v int) int { return it.Y + it.W*v/200 }
		c.Box = []int{
			max(py(by0-pad), 0) * 1000 / sceneH, max(px(bx0-pad), 0) * 1000 / sceneW,
			min(py(by1+pad)*1000/sceneH, 1000), min(px(bx1+pad)*1000/sceneW, 1000),
		}
		out = append(out, c)
	}
	return out
}

// storyFor finds the story metadata for a detected item by matching its
// category and colour to the scene's placements. It returns nil when the scene
// metadata does not know the item (for example a live detection of something
// the studio never tagged).
func storyFor(cat *Catalog, ad *Ad, sc *Scene, c Candidate) *Story {
	wantCat, wantColor := normCategory(c.Category), normColor(c.Color)
	var byCategory *Placement
	n := 0
	for i := range sc.Items {
		it := &sc.Items[i]
		if it.Character == "" {
			continue
		}
		category, _, _ := it.category(cat)
		if category != wantCat {
			continue
		}
		if it.Color == wantColor {
			return &Story{Kind: it.Role, Character: it.Character, Moment: it.Moment, Show: ad.Title, Episode: ad.Episode}
		}
		byCategory = it
		n++
	}
	if n == 1 {
		return &Story{Kind: byCategory.Role, Character: byCategory.Character, Moment: byCategory.Moment, Show: ad.Title, Episode: ad.Episode}
	}
	return nil
}
