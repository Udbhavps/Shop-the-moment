package main

// Vector artwork for the demo. The catalog thumbnails and the sample "ad"
// scenes are generated from the same product drawings, so what a viewer sees in
// the ad and what the panel shows for an exact match really do look alike.
// All artwork is original, team-made and uses fictional brands.

import (
	"fmt"
	"strconv"
	"strings"
)

func parseHex(h string) (r, g, b int) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 128, 128, 128
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	return int(v >> 16 & 255), int(v >> 8 & 255), int(v & 255)
}

// shade lightens (pct>0) or darkens (pct<0) a #RRGGBB colour.
func shade(h string, pct int) string {
	r, g, b := parseHex(h)
	f := func(c int) int {
		if pct >= 0 {
			c = c + (255-c)*pct/100
		} else {
			c = c * (100 + pct) / 100
		}
		return min(max(c, 0), 255)
	}
	return fmt.Sprintf("#%02X%02X%02X", f(r), f(g), f(b))
}

func contrast(h string) string {
	r, g, b := parseHex(h)
	if (r*299+g*587+b*114)/1000 > 150 {
		return "#16181d"
	}
	return "#ffffff"
}

// drawProduct returns SVG markup for one product in a 200x200 box.
// brand is the wordmark printed on the product ("" hides it, which is how the
// demo creates a product whose brand is not legible).
func drawProduct(category, id, color, brand string) string {
	base, ok := colorHex[color]
	if !ok {
		base = colorHex["grey"]
	}
	dark := shade(base, -22)
	darker := shade(base, -40)
	ink := contrast(base)
	word := strings.ToUpper(brand)
	text := func(x, y int, size int, fill string, anchor string) string {
		if word == "" {
			return ""
		}
		return fmt.Sprintf(`<text x="%d" y="%d" font-family="Helvetica, Arial, sans-serif" font-size="%d" font-weight="800" letter-spacing="1" text-anchor="%s" fill="%s">%s</text>`, x, y, size, anchor, fill, word)
	}
	var b strings.Builder
	switch category {
	case "sneakers":
		accent := "#F28C28"
		if color == "orange" {
			accent = "#ffffff"
		}
		fmt.Fprintf(&b, `<path d="M16 142 L16 133 Q16 129 22 129 L178 129 Q192 129 192 140 Q192 152 176 152 L30 152 Q16 152 16 142 Z" fill="#F1F1EE" stroke="#cfcfca" stroke-width="1.5"/>`)
		fmt.Fprintf(&b, `<path d="M22 130 L24 98 Q25 89 34 89 L58 87 Q65 87 69 92 L76 99 Q88 105 102 107 L148 112 Q186 118 191 130 Z" fill="%s"/>`, base)
		fmt.Fprintf(&b, `<path d="M150 113 Q186 118 191 130 L150 130 Z" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<path d="M22 130 L24 98 Q25 89 34 89 L46 89 L46 130 Z" fill="%s" opacity=".55"/>`, dark)
		fmt.Fprintf(&b, `<path d="M52 127 Q92 106 142 125" stroke="%s" stroke-width="7" fill="none" stroke-linecap="round"/>`, accent)
		fmt.Fprintf(&b, `<path d="M82 103 l10 -7 M94 107 l10 -7 M107 109 l10 -7 M120 112 l10 -7" stroke="%s" stroke-width="3" stroke-linecap="round" opacity=".9"/>`, ink)
		if word != "" {
			fmt.Fprintf(&b, `<text x="56" y="120" font-family="Helvetica, Arial, sans-serif" font-size="11" font-weight="800" letter-spacing="1" fill="%s">%s</text>`, ink, word)
		}
	case "backpack":
		fmt.Fprintf(&b, `<path d="M56 30 Q100 6 144 30" stroke="%s" stroke-width="6" fill="none" stroke-linecap="round"/>`, darker)
		fmt.Fprintf(&b, `<rect x="46" y="34" width="108" height="140" rx="32" fill="%s"/>`, base)
		fmt.Fprintf(&b, `<rect x="50" y="26" width="100" height="34" rx="16" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<rect x="92" y="52" width="16" height="12" rx="3" fill="%s"/>`, darker)
		fmt.Fprintf(&b, `<rect x="62" y="104" width="76" height="56" rx="14" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<path d="M66 118 L134 118" stroke="%s" stroke-width="2" stroke-dasharray="4 3"/>`, darker)
		fmt.Fprintf(&b, `<path d="M46 66 Q26 112 42 164" stroke="%s" stroke-width="9" fill="none" stroke-linecap="round"/>`, darker)
		fmt.Fprintf(&b, `<path d="M154 66 Q174 112 158 164" stroke="%s" stroke-width="9" fill="none" stroke-linecap="round"/>`, darker)
		b.WriteString(text(100, 90, 12, ink, "middle"))
	case "headphones":
		fmt.Fprintf(&b, `<path d="M46 116 Q46 34 100 34 Q154 34 154 116" stroke="%s" stroke-width="13" fill="none" stroke-linecap="round"/>`, darker)
		fmt.Fprintf(&b, `<path d="M52 108 Q54 46 100 46 Q146 46 148 108" stroke="%s" stroke-width="5" fill="none" stroke-linecap="round" opacity=".7"/>`, shade(base, 25))
		for _, cx := range []int{40, 160} {
			fmt.Fprintf(&b, `<ellipse cx="%d" cy="122" rx="24" ry="34" fill="%s"/>`, cx, base)
			fmt.Fprintf(&b, `<ellipse cx="%d" cy="122" rx="13" ry="22" fill="%s"/>`, cx, dark)
		}
		if word != "" {
			fmt.Fprintf(&b, `<rect x="72" y="24" width="56" height="18" rx="9" fill="%s"/>`, dark)
			b.WriteString(text(100, 37, 10, ink, "middle"))
		}
	case "smartwatch":
		fmt.Fprintf(&b, `<rect x="76" y="8" width="48" height="66" rx="12" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<rect x="76" y="126" width="48" height="66" rx="12" fill="%s"/>`, dark)
		rx := 40
		if id == "tempo-lite" {
			rx = 16
		}
		fmt.Fprintf(&b, `<rect x="140" y="88" width="7" height="16" rx="3" fill="%s"/>`, darker)
		fmt.Fprintf(&b, `<rect x="56" y="56" width="88" height="88" rx="%d" fill="%s"/>`, rx, darker)
		fmt.Fprintf(&b, `<rect x="64" y="64" width="72" height="72" rx="%d" fill="#0c0f16"/>`, max(rx-8, 8))
		b.WriteString(`<text x="100" y="104" font-family="Helvetica, Arial, sans-serif" font-size="19" font-weight="700" text-anchor="middle" fill="#7ee3ff">10:09</text>`)
		if word != "" {
			fmt.Fprintf(&b, `<text x="100" y="122" font-family="Helvetica, Arial, sans-serif" font-size="8" font-weight="800" letter-spacing="1.5" text-anchor="middle" fill="#9aa3b2">%s</text>`, word)
		}
	case "sunglasses":
		lens := `<defs><linearGradient id="lg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#2d3a4a"/><stop offset="1" stop-color="#0f141c"/></linearGradient></defs>`
		b.WriteString(lens)
		if id == "lumo-aviator" {
			fmt.Fprintf(&b, `<path d="M26 74 Q26 58 54 58 Q86 58 92 88 Q88 122 58 120 Q26 116 26 74 Z" fill="url(#lg)" stroke="%s" stroke-width="4"/>`, base)
			fmt.Fprintf(&b, `<path d="M174 74 Q174 58 146 58 Q114 58 108 88 Q112 122 142 120 Q174 116 174 74 Z" fill="url(#lg)" stroke="%s" stroke-width="4"/>`, base)
			fmt.Fprintf(&b, `<path d="M92 76 Q100 68 108 76" stroke="%s" stroke-width="4" fill="none"/>`, base)
			fmt.Fprintf(&b, `<path d="M26 70 L10 66 M174 70 L190 66" stroke="%s" stroke-width="4" stroke-linecap="round"/>`, base)
		} else {
			fmt.Fprintf(&b, `<rect x="22" y="64" width="68" height="52" rx="14" fill="url(#lg)" stroke="%s" stroke-width="9"/>`, base)
			fmt.Fprintf(&b, `<rect x="110" y="64" width="68" height="52" rx="14" fill="url(#lg)" stroke="%s" stroke-width="9"/>`, base)
			fmt.Fprintf(&b, `<rect x="90" y="72" width="20" height="8" rx="3" fill="%s"/>`, base)
			fmt.Fprintf(&b, `<path d="M22 72 L8 68 M178 72 L192 68" stroke="%s" stroke-width="7" stroke-linecap="round"/>`, base)
		}
		fmt.Fprintf(&b, `<path d="M36 82 L52 72" stroke="#fff" stroke-width="3" opacity=".35" stroke-linecap="round"/>`)
		if word != "" {
			fmt.Fprintf(&b, `<text x="100" y="148" font-family="Helvetica, Arial, sans-serif" font-size="11" font-weight="800" letter-spacing="2" text-anchor="middle" fill="%s">%s</text>`, "#6b7280", word)
		}
	case "water_bottle":
		fmt.Fprintf(&b, `<rect x="82" y="12" width="36" height="22" rx="6" fill="%s"/>`, darker)
		fmt.Fprintf(&b, `<rect x="86" y="30" width="28" height="18" rx="4" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<rect x="66" y="44" width="68" height="144" rx="18" fill="%s"/>`, base)
		fmt.Fprintf(&b, `<rect x="66" y="100" width="68" height="38" fill="%s" opacity=".5"/>`, dark)
		fmt.Fprintf(&b, `<rect x="76" y="58" width="7" height="116" rx="3" fill="#fff" opacity=".28"/>`)
		b.WriteString(text(100, 124, 12, ink, "middle"))
	case "jacket":
		fmt.Fprintf(&b, `<path d="M58 38 Q100 56 142 38 L166 52 L190 150 L164 158 L152 112 L152 178 L48 178 L48 112 L36 158 L10 150 L34 52 Z" fill="%s"/>`, base)
		fmt.Fprintf(&b, `<path d="M58 38 Q100 0 142 38 Q100 66 58 38 Z" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<path d="M100 56 L100 178" stroke="%s" stroke-width="3"/>`, darker)
		fmt.Fprintf(&b, `<path d="M60 128 L84 128 M116 128 L140 128" stroke="%s" stroke-width="3" stroke-linecap="round"/>`, darker)
		if word != "" {
			fmt.Fprintf(&b, `<rect x="101" y="70" width="54" height="15" rx="3" fill="%s"/>`, darker)
			fmt.Fprintf(&b, `<text x="128" y="80.5" font-family="Helvetica, Arial, sans-serif" font-size="6.8" font-weight="800" letter-spacing=".3" text-anchor="middle" fill="#fff">%s</text>`, word)
		}
	case "cap":
		fmt.Fprintf(&b, `<path d="M38 112 Q38 48 100 48 Q162 48 162 112 Z" fill="%s"/>`, base)
		fmt.Fprintf(&b, `<path d="M100 50 L100 110" stroke="%s" stroke-width="2" opacity=".5"/>`, darker)
		fmt.Fprintf(&b, `<path d="M28 114 Q100 100 176 124 Q180 142 152 144 Q92 130 28 128 Z" fill="%s"/>`, dark)
		fmt.Fprintf(&b, `<circle cx="100" cy="49" r="5" fill="%s"/>`, darker)
		b.WriteString(text(100, 94, 15, ink, "middle"))
	case "pendant":
		fmt.Fprintf(&b, `<path d="M34 24 Q36 108 100 120 Q164 108 166 24" fill="none" stroke="%s" stroke-width="4" stroke-linecap="round" stroke-dasharray="1 6"/>`, shade(base, 25))
		fmt.Fprintf(&b, `<circle cx="100" cy="150" r="40" fill="%s"/><circle cx="100" cy="150" r="33" fill="%s"/>`, dark, base)
		fmt.Fprintf(&b, `<circle cx="100" cy="116" r="6" fill="none" stroke="%s" stroke-width="3"/>`, dark)
		fmt.Fprintf(&b, `<path d="M100 122 L106 144 L128 150 L106 156 L100 178 L94 156 L72 150 L94 144 Z" fill="%s"/>`, darker)
		if word != "" {
			fmt.Fprintf(&b, `<text x="100" y="196" font-family="Helvetica, Arial, sans-serif" font-size="9" font-weight="800" letter-spacing="2" text-anchor="middle" fill="#e8d9c4">%s</text>`, word)
		}
	case "lamp":
		b.WriteString(`<defs><radialGradient id="glow" cx=".5" cy=".5" r=".5"><stop offset="0" stop-color="#ffe9b0" stop-opacity=".85"/><stop offset="1" stop-color="#ffe9b0" stop-opacity="0"/></radialGradient></defs>`)
		b.WriteString(`<circle cx="100" cy="72" r="72" fill="url(#glow)"/>`)
		fmt.Fprintf(&b, `<ellipse cx="100" cy="182" rx="46" ry="10" fill="#3b3028"/>`)
		fmt.Fprintf(&b, `<rect x="94" y="96" width="12" height="84" rx="4" fill="#4a3c30"/>`)
		fmt.Fprintf(&b, `<path d="M58 28 L142 28 L170 98 L30 98 Z" fill="#F3D9A4"/>`)
		if word != "" {
			fmt.Fprintf(&b, `<text x="100" y="186" font-family="Helvetica, Arial, sans-serif" font-size="9" font-weight="800" letter-spacing="2" text-anchor="middle" fill="#e8d9c4">%s</text>`, word)
		}
	}
	return b.String()
}

// productThumbSVG renders a catalog thumbnail: a tile with the drawing zoomed
// so the product fills most of it.
func productThumbSVG(p Product, color string) string {
	if !p.HasVariant(color) {
		color = p.DefaultColor()
	}
	x0, y0, x1, y1 := artBounds(p.Category)
	bw, bh := x1-x0, y1-y0
	side := float64(max(bw, bh)) * 1.14
	vx := float64(x0) + float64(bw)/2 - side/2
	vy := float64(y0) + float64(bh)/2 - side/2
	scale := 200 / side                        // drawing units -> tile units
	shadowY := 14 + (float64(y1)-vy)*scale + 7 // just under the product
	shadowRX := float64(bw) * scale * 0.42
	// Dark products sit on a light tile and light ones on a dark tile, so every
	// colour variant stays readable.
	tileTop, tileBottom, shadowOp := "#2a2f3a", "#1a1d25", ".35"
	if r, g, b := parseHex(colorHex[color]); (r*299+g*587+b*114)/1000 < 112 {
		tileTop, tileBottom, shadowOp = "#e4e8ef", "#c9cfdb", ".18"
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 240" role="img" aria-label="%s">`+
		`<defs><linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>`+
		`<rect width="240" height="240" rx="24" fill="url(#bg)"/>`+
		`<ellipse cx="120" cy="%.1f" rx="%.1f" ry="8" fill="#000" opacity="%s"/>`+
		`<svg x="20" y="14" width="200" height="200" viewBox="%.1f %.1f %.1f %.1f" overflow="visible">%s</svg></svg>`,
		xmlEscape(p.Name+" in "+color), tileTop, tileBottom, shadowY, shadowRX, shadowOp, vx, vy, side, side, drawProduct(p.Category, p.ID, color, p.Brand))
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// groundY returns where the bottom of a drawing sits inside its 200x200 box,
// so shadows in the ad scenes land under the object instead of floating.
func groundY(category string) int {
	switch category {
	case "sneakers":
		return 152
	case "backpack":
		return 176
	case "headphones":
		return 158
	case "smartwatch":
		return 192
	case "sunglasses":
		return 152
	case "water_bottle":
		return 188
	case "jacket":
		return 178
	case "cap":
		return 146
	case "pendant":
		return 192
	case "lamp":
		return 190
	}
	return 180
}

// artBounds returns the drawn extent of a product inside its 200x200 box as
// x0, y0, x1, y1. Thumbnails zoom to it and the cached detections use it for
// tight bounding boxes.
func artBounds(category string) (x0, y0, x1, y1 int) {
	switch category {
	case "sneakers":
		return 14, 84, 194, 154
	case "backpack":
		return 24, 10, 176, 178
	case "headphones":
		return 14, 26, 186, 160
	case "smartwatch":
		return 54, 6, 150, 194
	case "sunglasses":
		return 6, 54, 194, 126
	case "water_bottle":
		return 64, 10, 136, 190
	case "jacket":
		return 8, 4, 192, 180
	case "cap":
		return 26, 44, 180, 148
	case "pendant":
		return 30, 20, 170, 200
	case "lamp":
		return 28, 24, 172, 194
	}
	return 0, 0, 200, 200
}
