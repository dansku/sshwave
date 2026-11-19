package sshconfig

import "strings"

// FuzzyScore returns a score above 0 when every rune of query appears in text
// in order. Higher is better. Prefix and consecutive matches score more.
func FuzzyScore(query, text string) int {
	q := []rune(strings.ToLower(query))
	if len(q) == 0 {
		return 1
	}
	t := []rune(strings.ToLower(text))
	score, qi, run := 0, 0, 0
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if t[ti] != q[qi] {
			run = 0
			continue
		}
		qi++
		run++
		score += 1 + run*2
		if ti == 0 {
			score += 6
		} else if t[ti-1] == '-' || t[ti-1] == '.' || t[ti-1] == '_' {
			score += 3
		}
	}
	if qi < len(q) {
		return 0
	}
	return score
}
