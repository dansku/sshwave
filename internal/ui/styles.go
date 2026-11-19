package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The synthwave theme. All colors live here.
var (
	cBg      = lipgloss.Color("#1a1033") // deep indigo
	cPanel   = lipgloss.Color("#2a1a52") // selected row
	cBorder  = lipgloss.Color("#4b2e83")
	cMagenta = lipgloss.Color("#ff2e97")
	cCyan    = lipgloss.Color("#00e5ff")
	cOrange  = lipgloss.Color("#ffb86c")
	cText    = lipgloss.Color("#e8e1ff")
	cMuted   = lipgloss.Color("#8a7fb0")
	cGreen   = lipgloss.Color("#5af78e")
	cRed     = lipgloss.Color("#ff5370")
)

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

var (
	sText   = fg(cText)
	sMuted  = fg(cMuted)
	sBold   = fg(cText).Bold(true)
	sAccent = fg(cMagenta).Bold(true)
	sCyan   = fg(cCyan)
	sTitle  = fg(cCyan).Bold(true)
	sWarn   = fg(cOrange)
	sBorder = fg(cBorder)
	sKey    = lipgloss.NewStyle().Foreground(cBg).Background(cMagenta).Bold(true)
)

// gradient colors the text rune by rune from a to b.
func gradient(s string, a, b color.Color) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return ""
	}
	cols := lipgloss.Blend1D(len(rs), a, b)
	out := ""
	for i, r := range rs {
		out += fg(cols[i]).Bold(true).Render(string(r))
	}
	return out
}
