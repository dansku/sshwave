package ui

import (
	"strings"
	"unicode"
)

// sanitize makes text from files, from `ssh -G`, and from errors safe to
// print. It replaces each control character (C0, DEL, C1), each format
// character (bidi controls, zero-width characters), and each line or
// paragraph separator with a visible "·". Without it, such text could send
// escape sequences to the terminal or reorder what the user sees. Call it on
// raw text, before styles are applied.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return '·'
		}
		return r
	}, s)
}
