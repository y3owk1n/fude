// Package fuzzy ranks text by how well a query's letters match it, in
// order and ignoring case, as fzy and Vim's matchfuzzy() do (B129, B152).
package fuzzy

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// Match returns the paths query matches, the best first, and all of them
// for no query.
func Match(paths []string, query string) []string {
	if query == "" {
		return paths
	}
	type scored struct {
		path  string
		score float64
	}
	var found []scored
	q, scratch := Lower(query), []float64(nil)
	for _, p := range paths {
		if s, ok := fuzzy(q, p, &scratch); ok {
			found = append(found, scored{p, s})
		}
	}
	slices.SortStableFunc(found, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]string, len(found))
	for i, f := range found {
		out[i] = f.path
	}
	return out
}

// The scores of fzy's algorithm, which ranks the finder's matches (B129).
const (
	gapLeading   = -0.005
	gapTrailing  = -0.005
	gapInner     = -0.01
	matchRun     = 1.0
	matchSlash   = 0.9
	matchWord    = 0.8
	matchCapital = 0.7
	matchDot     = 0.6
)

// fuzzy scores how well q, in lower case, matches s, ignoring case, or
// reports false when s lacks q's letters in order. A letter scores more
// after a separator or as a capital, and runs of letters score more than
// scattered ones. scratch is memory to reuse between calls.
func fuzzy(q, s string, scratch *[]float64) (float64, bool) {
	i := 0
	for j := 0; j < len(s) && i < len(q); j++ {
		if lowerByte(s[j]) == q[i] {
			i++
		}
	}
	if i < len(q) {
		return 0, false
	}
	if len(q) == len(s) {
		return math.MaxFloat64, true
	}
	n := len(s)
	if cap(*scratch) < 5*n {
		*scratch = make([]float64, 5*n)
	}
	buf := (*scratch)[:5*n]
	bonus, d, m, pd, pm := buf[:n], buf[n:2*n], buf[2*n:3*n], buf[3*n:4*n], buf[4*n:]
	bonuses(s, bonus)
	for i := range len(q) {
		row(q, i, s, bonus, pd, pm, d, m)
		d, pd = pd, d
		m, pm = pm, m
	}
	return pm[n-1], true
}

// Positions returns where in s each letter of q, in lower case, matches
// for its best score, or nil when s lacks them, so a list can mark them
// (B152).
func Positions(q, s string) []int {
	if _, ok := fuzzy(q, s, new([]float64)); !ok || q == "" {
		return nil
	}
	n := len(s)
	bonus := make([]float64, n)
	bonuses(s, bonus)
	d, m := make([][]float64, len(q)), make([][]float64, len(q))
	for i := range len(q) {
		d[i], m[i] = make([]float64, n), make([]float64, n)
		var pd, pm []float64
		if i > 0 {
			pd, pm = d[i-1], m[i-1]
		}
		row(q, i, s, bonus, pd, pm, d[i], m[i])
	}
	// Back from the end, each letter takes the last place that leads to the
	// best score, or the place a run of letters needs.
	out := make([]int, len(q))
	run := false
	j := n - 1
	for i := len(q) - 1; i >= 0; i-- {
		for ; j >= 0; j-- {
			if !math.IsInf(d[i][j], -1) && (run || d[i][j] == m[i][j]) {
				run = i > 0 && j > 0 && m[i][j] == d[i-1][j-1]+matchRun
				out[i] = j
				j--
				break
			}
		}
	}
	return out
}

// Occurrences returns the bytes of each place in s from start that query
// is at, ignoring case for a query without capitals, as grep searched.
func Occurrences(s, query string, start int) []int {
	hay := s[start:]
	if query == Lower(query) {
		hay = Lower(hay)
	}
	var out []int
	for at := 0; query != ""; at += len(query) {
		i := strings.Index(hay[at:], query)
		if i < 0 {
			break
		}
		at += i
		for k := range len(query) {
			out = append(out, start+at+k)
		}
	}
	return out
}

// bonuses fills bonus with what a match at each byte of s scores for the
// byte before it: after a separator, or a capital after a lower case letter.
func bonuses(s string, bonus []float64) {
	clear(bonus)
	prev := byte('/')
	for j := range len(s) {
		c := s[j]
		switch {
		case prev == '/':
			bonus[j] = matchSlash
		case prev == '-' || prev == '_' || prev == ' ':
			bonus[j] = matchWord
		case prev == '.':
			bonus[j] = matchDot
		case prev >= 'a' && prev <= 'z' && c >= 'A' && c <= 'Z':
			bonus[j] = matchCapital
		}
		prev = c
	}
}

// row fills d and m for q's letter i from pd and pm, the row of q[i-1]: d
// is the best score with q[i] matched at s[j], and m the best with
// q[:i+1] matched by s[:j+1].
func row(q string, i int, s string, bonus, pd, pm, d, m []float64) {
	best := math.Inf(-1)
	gap := gapInner
	if i == len(q)-1 {
		gap = gapTrailing
	}
	for j := range len(s) {
		switch {
		case q[i] != lowerByte(s[j]):
			d[j] = math.Inf(-1)
		case i == 0:
			d[j] = float64(j)*gapLeading + bonus[j]
		case j > 0:
			d[j] = max(pm[j-1]+bonus[j], pd[j-1]+matchRun)
		default:
			d[j] = math.Inf(-1)
		}
		best = max(d[j], best+gap)
		m[j] = best
	}
}

// Lower returns s with ASCII letters in lower case, byte for byte.
func Lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		b[i] = lowerByte(c)
	}
	return string(b)
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
