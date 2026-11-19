package ui

import (
	"flag"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"
	"testing"
)

// screenshot writes the README picture. Run `make screenshot`. It renders the
// fixture in testdata/ssh-notes (fake data only) as a colored SVG: one text
// element per cell, so the columns line up in any font.
var screenshot = flag.String("screenshot", "", "write the README screenshot (an SVG) to this path")

type cell struct {
	r              rune
	fg, bg         string // "#rrggbb", or "" for the default
	bold, dim, und bool
	rev            bool
}

const (
	shotFg = "#e8e1ff" // cText
	shotBg = "#1a1033" // cBg
)

// parseANSI turns text with SGR codes into rows of cells. It reads the codes
// that Lip Gloss writes: reset, bold, dim, underline, reverse, and 24-bit,
// 256-color and 16-color foreground and background.
func parseANSI(s string, cols int) [][]cell {
	var rows [][]cell
	var cur cell
	row := []cell{}
	flush := func() {
		for len(row) < cols {
			row = append(row, cell{r: ' '})
		}
		rows = append(rows, row)
		row = []cell{}
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '\x1b' && i+1 < len(rs) && rs[i+1] == '[':
			j := i + 2
			for j < len(rs) && (rs[j] < '@' || rs[j] > '~') {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				cur = applySGR(cur, string(rs[i+2:j]))
			}
			i = j
		case rs[i] == '\n':
			flush()
		default:
			c := cur
			c.r = rs[i]
			row = append(row, c)
		}
	}
	if len(row) > 0 {
		flush()
	}
	return rows
}

func applySGR(c cell, params string) cell {
	if params == "" {
		return cell{}
	}
	p := strings.Split(params, ";")
	num := func(k int) int {
		if k >= len(p) {
			return 0
		}
		n, _ := strconv.Atoi(p[k])
		return n
	}
	for k := 0; k < len(p); k++ {
		n := num(k)
		switch {
		case n == 0:
			c = cell{}
		case n == 1:
			c.bold = true
		case n == 2:
			c.dim = true
		case n == 4:
			c.und = true
		case n == 7:
			c.rev = true
		case n == 22:
			c.bold, c.dim = false, false
		case n == 24:
			c.und = false
		case n == 27:
			c.rev = false
		case n == 39:
			c.fg = ""
		case n == 49:
			c.bg = ""
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			c.fg = ansi16(n)
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			c.bg = ansi16(n - 10)
		case n == 38 || n == 48:
			var col string
			if num(k+1) == 2 {
				col = fmt.Sprintf("#%02x%02x%02x", num(k+2), num(k+3), num(k+4))
				k += 4
			} else if num(k+1) == 5 {
				col = ansi256(num(k + 2))
				k += 2
			}
			if n == 38 {
				c.fg = col
			} else {
				c.bg = col
			}
		}
	}
	return c
}

func ansi16(n int) string {
	pal := []string{"#000000", "#cd3131", "#0dbc79", "#e5e510", "#2472c8", "#bc3fbc", "#11a8cd", "#e5e5e5",
		"#666666", "#f14c4c", "#23d18b", "#f5f543", "#3b8eea", "#d670d6", "#29b8db", "#ffffff"}
	if n >= 90 {
		return pal[8+n-90]
	}
	return pal[n-30]
}

func ansi256(n int) string {
	switch {
	case n < 16:
		return ansi16Index(n)
	case n < 232:
		n -= 16
		lv := []int{0, 95, 135, 175, 215, 255}
		return fmt.Sprintf("#%02x%02x%02x", lv[n/36], lv[(n/6)%6], lv[n%6])
	default:
		g := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", g, g, g)
	}
}

func ansi16Index(n int) string {
	if n < 8 {
		return ansi16(30 + n)
	}
	return ansi16(90 + n - 8)
}

// svgScreenshot draws rows as a terminal window.
func svgScreenshot(rows [][]cell) string {
	const (
		cw, ch   = 9.6, 20.0
		pad      = 22.0
		bar      = 38.0
		fontSize = 16
	)
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	w := float64(cols)*cw + 2*pad
	h := float64(len(rows))*ch + 2*pad + bar
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" role="img" aria-label="The sshwave terminal UI showing a host list with groups and notes, and the details of one host">`+"\n", w, h, w, h)
	b.WriteString(`<defs><linearGradient id="glow" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#ff2e97"/><stop offset="1" stop-color="#00e5ff"/></linearGradient></defs>` + "\n")
	fmt.Fprintf(&b, `<rect width="%.0f" height="%.0f" rx="14" fill="%s"/>`+"\n", w, h, shotBg)
	fmt.Fprintf(&b, `<rect x="0.75" y="0.75" width="%.2f" height="%.2f" rx="13.25" fill="none" stroke="url(#glow)" stroke-opacity="0.55" stroke-width="1.5"/>`+"\n", w-1.5, h-1.5)
	for i, c := range []string{"#ff5370", "#ffb86c", "#5af78e"} {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%.0f" r="6" fill="%s"/>`, 24+i*20, bar/2, c)
	}
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" fill="#8a7fb0" font-size="13" text-anchor="middle" font-family="-apple-system, Segoe UI, Helvetica, Arial, sans-serif">sshwave</text>`+"\n", w/2, bar/2+4.5)
	fmt.Fprintf(&b, `<g font-family="JetBrains Mono, SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace" font-size="%d">`+"\n", fontSize)
	for y, row := range rows {
		top := bar + pad + float64(y)*ch
		// Backgrounds, merged per run.
		for x := 0; x < len(row); {
			bg, _ := colors(row[x])
			e := x
			for e < len(row) {
				if g, _ := colors(row[e]); g != bg {
					break
				}
				e++
			}
			if bg != shotBg {
				fmt.Fprintf(&b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`, pad+float64(x)*cw, top, float64(e-x)*cw, ch, bg)
			}
			x = e
		}
		b.WriteString("\n")
		for x, c := range row {
			if c.r == ' ' && !c.und {
				continue
			}
			_, fg := colors(c)
			attr := ""
			if c.bold {
				attr += ` font-weight="700"`
			}
			if c.dim {
				attr += ` opacity="0.6"`
			}
			if c.und {
				attr += ` text-decoration="underline"`
			}
			fmt.Fprintf(&b, `<text x="%.2f" y="%.2f" fill="%s"%s>%s</text>`, pad+float64(x)*cw, top+ch-5, fg, attr, html.EscapeString(string(c.r)))
		}
		b.WriteString("\n")
	}
	b.WriteString("</g>\n</svg>\n")
	return b.String()
}

// colors returns the background and foreground of a cell.
func colors(c cell) (bg, fg string) {
	bg, fg = c.bg, c.fg
	if bg == "" {
		bg = shotBg
	}
	if fg == "" {
		fg = shotFg
	}
	if c.rev {
		bg, fg = fg, bg
	}
	return bg, fg
}

func TestScreenshot(t *testing.T) {
	if *screenshot == "" {
		t.Skip("run with -screenshot <path>")
	}
	const w, h = 100, 28
	m := selectAlias(t, loadedFrom(noteFixture, w, h), "production-api")
	svg := svgScreenshot(parseANSI(m.render(), w))
	if err := os.WriteFile(*screenshot, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
}
