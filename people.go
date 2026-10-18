package main

// Flat, faceless people for the sample scenes, so the clips read as moments of
// a story rather than product shots. They are team-made, drawn behind the
// products and never wear catalog items, so they do not compete with detection.

import (
	"fmt"
	"strings"
)

type pt [2]float64

// pose is a stick-figure skeleton in a 100-unit-tall box, feet at (0,0), y up is negative.
type pose struct {
	shoulder pt       // shoulder centre (hip is fixed at 0,-48)
	head     pt       // head centre
	arms     [2][2]pt // elbow, hand for the back and front arm
	legs     [2][2]pt // knee, foot for the back and front leg
}

var poses = map[string]pose{
	"stand": {shoulder: pt{0, -79}, head: pt{0, -90},
		arms: [2][2]pt{{{-6, -64}, {-7, -49}}, {{6, -64}, {7, -49}}},
		legs: [2][2]pt{{{-3, -25}, {-4, 0}}, {{3, -25}, {4, 0}}}},
	"run": {shoulder: pt{7, -77}, head: pt{11, -88},
		arms: [2][2]pt{{{-8, -66}, {-16, -74}}, {{17, -66}, {25, -57}}},
		legs: [2][2]pt{{{-12, -33}, {-26, -16}}, {{14, -35}, {17, -9}}}},
	"walk": {shoulder: pt{3, -79}, head: pt{5, -90},
		arms: [2][2]pt{{{-3, -63}, {-9, -52}}, {{9, -63}, {13, -53}}},
		legs: [2][2]pt{{{-6, -26}, {-13, 0}}, {{6, -26}, {13, 0}}}},
	"cheer": {shoulder: pt{0, -79}, head: pt{0, -91},
		arms: [2][2]pt{{{-13, -90}, {-18, -106}}, {{13, -90}, {18, -106}}},
		legs: [2][2]pt{{{-6, -25}, {-10, 0}}, {{6, -25}, {10, 0}}}},
	"stretch": {shoulder: pt{0, -79}, head: pt{1, -91},
		arms: [2][2]pt{{{-4, -94}, {-2, -108}}, {{6, -94}, {3, -108}}},
		legs: [2][2]pt{{{-7, -25}, {-12, 0}}, {{7, -25}, {12, 0}}}},
	"listen": {shoulder: pt{0, -79}, head: pt{1, -90},
		arms: [2][2]pt{{{-6, -64}, {-7, -50}}, {{9, -72}, {5, -88}}},
		legs: [2][2]pt{{{-3, -25}, {-4, 0}}, {{3, -25}, {4, 0}}}},
	"pockets": {shoulder: pt{0, -79}, head: pt{1, -90},
		arms: [2][2]pt{{{-9, -63}, {-4, -50}}, {{9, -63}, {4, -50}}},
		legs: [2][2]pt{{{-4, -25}, {-6, 0}}, {{4, -25}, {6, 0}}}},
}

type figure struct {
	pose        string
	x, base, h  int
	top, bottom string
	skin, hair  string
	flip        bool
}

func poly(a pt, rest ...pt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "M%.1f %.1f", a[0], a[1])
	for _, p := range rest {
		fmt.Fprintf(&b, " L%.1f %.1f", p[0], p[1])
	}
	return b.String()
}

// dot is a filled disc drawn as a path: the clip renderer fades every <circle>
// in the background (the sun), and people must not fade with it.
func dot(cx, cy, r float64, fill string) string {
	return fmt.Sprintf(`<path d="M%.1f %.1f a%.1f %.1f 0 1 0 %.1f 0 a%.1f %.1f 0 1 0 %.1f 0z" fill="%s" stroke="none"/>`, cx-r, cy, r, r, 2*r, r, r, -2*r, fill)
}

func (f figure) svg() string {
	p := poses[f.pose]
	hip := pt{0, -48}
	k := float64(f.h) / 100
	sx := k
	if f.flip {
		sx = -k
	}
	darkTop := shade(f.top, -25)
	var b strings.Builder
	fmt.Fprintf(&b, `<g transform="translate(%d %d) scale(%.3f %.3f)" stroke-linecap="round" stroke-linejoin="round" fill="none">`, f.x, f.base, sx, k)
	// back arm and leg
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="6.5"/>`, poly(p.shoulder, p.arms[0][0], p.arms[0][1]), darkTop)
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="9.5"/>`, poly(hip, p.legs[0][0], p.legs[0][1]), shade(f.bottom, -25))
	// torso and neck
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="17"/>`, poly(hip, p.shoulder), f.top)
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="5"/>`, poly(p.shoulder, p.head), f.skin)
	// front leg and arm
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="9.5"/>`, poly(hip, p.legs[1][0], p.legs[1][1]), f.bottom)
	fmt.Fprintf(&b, `<path d="%s" stroke="%s" stroke-width="6.5"/>`, poly(p.shoulder, p.arms[1][0], p.arms[1][1]), f.top)
	for _, a := range p.arms {
		b.WriteString(dot(a[1][0], a[1][1], 3.1, f.skin))
	}
	// head and hair
	b.WriteString(dot(p.head[0], p.head[1], 7.6, f.skin))
	fmt.Fprintf(&b, `<path d="M%.1f %.1f A7.9 7.9 0 0 1 %.1f %.1f L%.1f %.1f Q%.1f %.1f %.1f %.1f Z" fill="%s" stroke="none"/>`,
		p.head[0]-7.9, p.head[1]+1, p.head[0]+7.9, p.head[1]+1, p.head[0]+7.9, p.head[1]-1,
		p.head[0], p.head[1]-4.5, p.head[0]-7.9, p.head[1]-1, f.hair)
	b.WriteString(`</g>`)
	return b.String()
}

// scenePeople lists who appears in each sample scene. Positions sit in the gaps
// between products, behind them, so the shopping boxes stay clean.
var scenePeople = map[string][]figure{
	"morning-run-1":    {{"run", 705, 545, 290, "#2f5b66", "#233a44", "#b9794d", "#2a1a12", false}, {"run", 1190, 470, 120, "#6d4b73", "#2c3a50", "#e0ac69", "#1c1412", true}},
	"morning-run-2":    {{"stretch", 655, 548, 290, "#a24b3f", "#26333f", "#8d5524", "#16100d", false}},
	"weekend-escape-1": {{"walk", 672, 548, 285, "#36563f", "#2b3342", "#e0ac69", "#3a2515", false}, {"walk", 1195, 478, 118, "#5b4a8a", "#2b3342", "#b9794d", "#17110e", true}},
	"weekend-escape-2": {{"cheer", 705, 548, 290, "#2f4f7a", "#202a3a", "#c68642", "#1b130e", false}},
	"quiet-hours-1":    {{"listen", 585, 548, 300, "#6a5fa3", "#1f1d3a", "#e0ac69", "#2a1b12", false}},
	"quiet-hours-2":    {{"pockets", 575, 548, 300, "#8b86c8", "#25224a", "#8d5524", "#120d0b", false}},
	"neon-harbor-1":    {{"stand", 640, 552, 300, "#16283d", "#0d1829", "#a8754f", "#0b0907", false}, {"walk", 935, 482, 185, "#1a3248", "#0f1c2e", "#c68642", "#0d0a08", true}},
	"neon-harbor-2":    {{"pockets", 610, 552, 310, "#3b2658", "#180f2a", "#e0ac69", "#120d0b", false}},
}

// drawPeople returns the figures for a scene, drawn before the products.
// Each figure sits in a group; no <ellipse> is used so the clip renderer can
// tell the background from the first product shadow.
func drawPeople(sceneID string) string {
	var b strings.Builder
	for _, f := range scenePeople[sceneID] {
		// a soft ground shadow made from a path, not an ellipse
		w := float64(f.h) * 0.2
		fmt.Fprintf(&b, `<path d="M%.0f %d q%.0f -9 %.0f 0 q%.0f 9 %.0f 0z" fill="#000" opacity=".22"/>`, float64(f.x)-w, f.base+3, w, w*2, -w, -w*2)
		b.WriteString(f.svg())
	}
	return b.String()
}
