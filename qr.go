package main

// A small, dependency-free QR Code encoder (ISO/IEC 18004): byte mode, error
// correction level M, versions 1-10 (up to 213 bytes). It is enough for the
// short session URLs used by the TV-to-phone handoff. Correctness is checked
// in qr_test.go by decoding the output with OpenCV when it is available.

import (
	"errors"
	"fmt"
	"strings"
)

type qrBlockSpec struct {
	ecPerBlock int
	groups     [][2]int // {block count, data codewords per block}
}

// Level M specification for versions 1..10.
var qrSpecM = [11]qrBlockSpec{
	{},
	{10, [][2]int{{1, 16}}},
	{16, [][2]int{{1, 28}}},
	{26, [][2]int{{1, 44}}},
	{18, [][2]int{{2, 32}}},
	{24, [][2]int{{2, 43}}},
	{16, [][2]int{{4, 27}}},
	{18, [][2]int{{4, 31}}},
	{22, [][2]int{{2, 38}, {2, 39}}},
	{22, [][2]int{{3, 36}, {2, 37}}},
	{26, [][2]int{{4, 43}, {1, 44}}},
}

var qrAlign = [11][]int{
	{}, {}, {6, 18}, {6, 22}, {6, 26}, {6, 30}, {6, 34}, {6, 22, 38}, {6, 24, 42}, {6, 26, 46}, {6, 28, 50},
}

func (s qrBlockSpec) dataCodewords() int {
	n := 0
	for _, g := range s.groups {
		n += g[0] * g[1]
	}
	return n
}

// --- GF(256) arithmetic, primitive polynomial x^8+x^4+x^3+x^2+1 -----------

var gfExp [512]byte
var gfLog [256]int

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[gfLog[a]+gfLog[b]]
}

func rsGenerator(degree int) []byte {
	g := []byte{1}
	for i := 0; i < degree; i++ {
		next := make([]byte, len(g)+1)
		for j, c := range g {
			next[j] ^= c
			next[j+1] ^= gfMul(c, gfExp[i])
		}
		g = next
	}
	return g // highest-degree coefficient first
}

func rsRemainder(data []byte, degree int) []byte {
	gen := rsGenerator(degree)
	rem := make([]byte, degree)
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[degree-1] = 0
		for i := 0; i < degree; i++ {
			rem[i] ^= gfMul(gen[i+1], factor)
		}
	}
	return rem
}

// --- bit buffer -----------------------------------------------------------

type bitBuf struct{ bits []bool }

func (b *bitBuf) add(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		b.bits = append(b.bits, (v>>uint(i))&1 == 1)
	}
}

func (b *bitBuf) bytes() []byte {
	out := make([]byte, (len(b.bits)+7)/8)
	for i, bit := range b.bits {
		if bit {
			out[i/8] |= 0x80 >> uint(i%8)
		}
	}
	return out
}

// --- matrix ---------------------------------------------------------------

type qrMatrix struct {
	size int
	mod  [][]bool
	fn   [][]bool // function-pattern modules (not masked, not data)
}

func newQRMatrix(version int) *qrMatrix {
	size := 17 + 4*version
	m := &qrMatrix{size: size}
	m.mod = make([][]bool, size)
	m.fn = make([][]bool, size)
	for i := range m.mod {
		m.mod[i] = make([]bool, size)
		m.fn[i] = make([]bool, size)
	}
	return m
}

func (m *qrMatrix) setFn(x, y int, dark bool) {
	if x < 0 || y < 0 || x >= m.size || y >= m.size {
		return
	}
	m.mod[y][x] = dark
	m.fn[y][x] = true
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func (m *qrMatrix) drawFinder(cx, cy int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			d := max(abs(dx), abs(dy))
			m.setFn(cx+dx, cy+dy, d != 2 && d != 4)
		}
	}
}

func (m *qrMatrix) drawAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			m.setFn(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
		}
	}
}

func bitAt(v uint32, i int) bool { return (v>>uint(i))&1 == 1 }

func (m *qrMatrix) drawFormat(mask int) {
	data := uint32(0<<3 | mask) // EC level M = 00
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	for i := 0; i <= 5; i++ {
		m.setFn(8, i, bitAt(bits, i))
	}
	m.setFn(8, 7, bitAt(bits, 6))
	m.setFn(8, 8, bitAt(bits, 7))
	m.setFn(7, 8, bitAt(bits, 8))
	for i := 9; i < 15; i++ {
		m.setFn(14-i, 8, bitAt(bits, i))
	}
	for i := 0; i < 8; i++ {
		m.setFn(m.size-1-i, 8, bitAt(bits, i))
	}
	for i := 8; i < 15; i++ {
		m.setFn(8, m.size-15+i, bitAt(bits, i))
	}
	m.setFn(8, m.size-8, true) // dark module
}

func (m *qrMatrix) drawVersion(version int) {
	if version < 7 {
		return
	}
	rem := uint32(version)
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	bits := uint32(version)<<12 | rem
	for i := 0; i < 18; i++ {
		c := bitAt(bits, i)
		a := m.size - 11 + i%3
		b := i / 3
		m.setFn(a, b, c)
		m.setFn(b, a, c)
	}
}

func (m *qrMatrix) drawFunctionPatterns(version int) {
	for i := 0; i < m.size; i++ {
		m.setFn(6, i, i%2 == 0)
		m.setFn(i, 6, i%2 == 0)
	}
	m.drawFinder(3, 3)
	m.drawFinder(m.size-4, 3)
	m.drawFinder(3, m.size-4)
	pos := qrAlign[version]
	for i, cy := range pos {
		for j, cx := range pos {
			if (i == 0 && j == 0) || (i == 0 && j == len(pos)-1) || (i == len(pos)-1 && j == 0) {
				continue
			}
			m.drawAlignment(cx, cy)
		}
	}
	m.drawFormat(0) // placeholder to reserve the areas; redrawn with the real mask
	m.drawVersion(version)
}

func (m *qrMatrix) placeData(data []byte) {
	i := 0
	for right := m.size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < m.size; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := ((right + 1) & 2) == 0
				y := vert
				if upward {
					y = m.size - 1 - vert
				}
				if !m.fn[y][x] && i < len(data)*8 {
					m.mod[y][x] = bitAt(uint32(data[i>>3]), 7-(i&7))
					i++
				}
			}
		}
	}
}

func maskBit(mask, x, y int) bool {
	switch mask {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	default:
		return ((x+y)%2+x*y%3)%2 == 0
	}
}

func (m *qrMatrix) clone() *qrMatrix {
	c := &qrMatrix{size: m.size, fn: m.fn}
	c.mod = make([][]bool, m.size)
	for i := range m.mod {
		c.mod[i] = append([]bool(nil), m.mod[i]...)
	}
	return c
}

func (m *qrMatrix) applyMask(mask int) {
	for y := 0; y < m.size; y++ {
		for x := 0; x < m.size; x++ {
			if !m.fn[y][x] && maskBit(mask, x, y) {
				m.mod[y][x] = !m.mod[y][x]
			}
		}
	}
}

func (m *qrMatrix) penalty() int {
	n := m.size
	score := 0
	run := func(get func(i int) bool) {
		count := 1
		for i := 1; i < n; i++ {
			if get(i) == get(i-1) {
				count++
			} else {
				if count >= 5 {
					score += 3 + count - 5
				}
				count = 1
			}
		}
		if count >= 5 {
			score += 3 + count - 5
		}
	}
	for y := 0; y < n; y++ {
		y := y
		run(func(i int) bool { return m.mod[y][i] })
	}
	for x := 0; x < n; x++ {
		x := x
		run(func(i int) bool { return m.mod[i][x] })
	}
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			c := m.mod[y][x]
			if c == m.mod[y][x+1] && c == m.mod[y+1][x] && c == m.mod[y+1][x+1] {
				score += 3
			}
		}
	}
	pat1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	pat2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	match := func(get func(i int) bool, start int, pat []bool) bool {
		for k, p := range pat {
			if get(start+k) != p {
				return false
			}
		}
		return true
	}
	for y := 0; y < n; y++ {
		y := y
		g := func(i int) bool { return m.mod[y][i] }
		for x := 0; x+11 <= n; x++ {
			if match(g, x, pat1) || match(g, x, pat2) {
				score += 40
			}
		}
	}
	for x := 0; x < n; x++ {
		x := x
		g := func(i int) bool { return m.mod[i][x] }
		for y := 0; y+11 <= n; y++ {
			if match(g, y, pat1) || match(g, y, pat2) {
				score += 40
			}
		}
	}
	dark := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if m.mod[y][x] {
				dark++
			}
		}
	}
	k := abs(dark*100/(n*n)-50) / 5
	return score + k*10
}

// EncodeQR returns the module matrix (true = dark) for text.
func EncodeQR(text string) ([][]bool, error) {
	data := []byte(text)
	version := 0
	for v := 1; v <= 10; v++ {
		cc := 8
		if v >= 10 {
			cc = 16
		}
		if 4+cc+8*len(data) <= qrSpecM[v].dataCodewords()*8 {
			version = v
			break
		}
	}
	if version == 0 {
		return nil, errors.New("qr: text too long (max 213 bytes)")
	}
	spec := qrSpecM[version]

	var bb bitBuf
	bb.add(0x4, 4) // byte mode
	if version >= 10 {
		bb.add(uint32(len(data)), 16)
	} else {
		bb.add(uint32(len(data)), 8)
	}
	for _, b := range data {
		bb.add(uint32(b), 8)
	}
	capBits := spec.dataCodewords() * 8
	bb.add(0, min(4, capBits-len(bb.bits)))
	for len(bb.bits)%8 != 0 {
		bb.bits = append(bb.bits, false)
	}
	cw := bb.bytes()
	for pad := byte(0xEC); len(cw) < spec.dataCodewords(); {
		cw = append(cw, pad)
		if pad == 0xEC {
			pad = 0x11
		} else {
			pad = 0xEC
		}
	}

	// Split into blocks, compute error correction, interleave.
	var blocks, ecs [][]byte
	off := 0
	for _, g := range spec.groups {
		for i := 0; i < g[0]; i++ {
			blk := cw[off : off+g[1]]
			off += g[1]
			blocks = append(blocks, blk)
			ecs = append(ecs, rsRemainder(blk, spec.ecPerBlock))
		}
	}
	var final []byte
	maxLen := 0
	for _, b := range blocks {
		maxLen = max(maxLen, len(b))
	}
	for i := 0; i < maxLen; i++ {
		for _, b := range blocks {
			if i < len(b) {
				final = append(final, b[i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for _, e := range ecs {
			final = append(final, e[i])
		}
	}

	base := newQRMatrix(version)
	base.drawFunctionPatterns(version)
	base.placeData(final)

	best, bestScore := (*qrMatrix)(nil), 1<<30
	for mask := 0; mask < 8; mask++ {
		c := base.clone()
		c.applyMask(mask)
		c.drawFormat(mask)
		if p := c.penalty(); p < bestScore {
			best, bestScore = c, p
		}
	}
	return best.mod, nil
}

// QRSVG renders text as a self-contained SVG with a 4-module quiet zone.
func QRSVG(text string) (string, error) {
	mod, err := EncodeQR(text)
	if err != nil {
		return "", err
	}
	n := len(mod)
	total := n + 8
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR code"><rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, total, total, total, total)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if mod[y][x] {
				fmt.Fprintf(&sb, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	sb.WriteString(`"/></svg>`)
	return sb.String(), nil
}
