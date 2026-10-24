// Package fuzzy ranks text by how well a query's letters match it, in
// order and ignoring case, as fzy and Vim's matchfuzzy() do (B129, B152).
package fuzzy

import (
	"math"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Match returns the paths query matches, the best first, and all of them
// for no query. A tie keeps the order of paths.
func Match(paths []string, query string) []string {
	if query == "" {
		return paths
	}
	q := Lower(query)
	workers := 1
	if len(paths) >= parallelFrom {
		workers = runtime.GOMAXPROCS(0)
	}
	// Each worker scores and sorts a run of paths, and the runs merge.
	size := (len(paths) + workers - 1) / workers
	runs := make([][]scored, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			var scratch []int32
			for i := w * size; i < min((w+1)*size, len(paths)); i++ {
				if s, ok := fuzzy(q, paths[i], &scratch); ok {
					runs[w] = append(runs[w], scored{i, s})
				}
			}
			slices.SortFunc(runs[w], better)
		})
	}
	wg.Wait()
	total := 0
	for _, r := range runs {
		total += len(r)
	}
	out := make([]string, 0, total)
	for {
		best := -1
		for w, r := range runs {
			if len(r) > 0 && (best < 0 || better(r[0], runs[best][0]) < 0) {
				best = w
			}
		}
		if best < 0 {
			return out
		}
		out = append(out, paths[runs[best][0].at])
		runs[best] = runs[best][1:]
	}
}

// parallelFrom is the fewest paths Match splits across cores. Fewer, such
// as a command line's words, match faster on one core.
const parallelFrom = 8192

// scored is the path at index at in Match's paths, and its score.
type scored struct {
	at    int
	score int32
}

// better orders a before b when it scores more, or ties and comes first.
func better(a, b scored) int {
	switch {
	case a.score > b.score:
		return -1
	case a.score < b.score:
		return 1
	}
	return a.at - b.at
}

// The scores of fzy's algorithm, which ranks the finder's matches (B129),
// in thousandths, so no rounding splits a tie. none marks a cell that no
// match reaches. Such a cell can drift above none by a few bonuses, and
// stays below none/2.
const (
	gapLeading   = -5
	gapTrailing  = -5
	gapInner     = -10
	matchRun     = 1000
	matchSlash   = 900
	matchWord    = 800
	matchCapital = 700
	matchDot     = 600
	none         = math.MinInt32 / 2
)

// fuzzy scores how well q, in lower case, matches s, ignoring case, or
// reports false when s lacks q's letters in order. A letter scores more
// after a separator or as a capital, and runs of letters score more than
// scattered ones. scratch is memory to reuse between calls.
func fuzzy(q, s string, scratch *[]int32) (int32, bool) {
	start, at := 0, 0
	for i := range len(q) {
		k := indexFold(s[at:], q[i])
		if k < 0 {
			return 0, false
		}
		if i == 0 {
			start = at + k
		}
		at += k + 1
	}
	if len(q) == len(s) {
		return math.MaxInt32, true
	}
	// A match lies between the first place of q's first letter and the last
	// place of its last letter. Each byte after that costs a trailing gap.
	end := len(s) - 1
	for lowerByte(s[end]) != q[len(q)-1] {
		end--
	}
	var score int32
	if len(q) == 1 {
		score = single(q[0], s, start, end)
	} else {
		n := end - start + 1
		if cap(*scratch) < 5*n {
			*scratch = make([]int32, 5*n)
		}
		buf := (*scratch)[:5*n]
		bonus, d, m, pd, pm := buf[:n], buf[n:2*n], buf[2*n:3*n], buf[3*n:4*n], buf[4*n:]
		bonuses(s, start, bonus)
		for i := range len(q) {
			row(q, i, s[start:end+1], start, bonus, pd, pm, d, m)
			d, pd = pd, d
			m, pm = pm, m
		}
		score = pm[n-1]
	}
	for range len(s) - 1 - end {
		score += gapTrailing
	}
	return score, true
}

// single scores the one letter c over s[start:end+1] as row does, with no
// rows to keep.
func single(c byte, s string, start, end int) int32 {
	best := int32(none)
	for j := start; j <= end; j++ {
		d := int32(none)
		if lowerByte(s[j]) == c {
			d = int32(j)*gapLeading + bonusAt(s, j)
		}
		best = max(d, best+gapTrailing, none)
	}
	return best
}

// indexFold returns the first byte of s that is c, a lower case byte, in
// either case, or -1.
func indexFold(s string, c byte) int {
	i := strings.IndexByte(s, c)
	if c >= 'a' && c <= 'z' {
		if i >= 0 {
			s = s[:i]
		}
		if k := strings.IndexByte(s, c-'a'+'A'); k >= 0 {
			return k
		}
	}
	return i
}

// Positions returns where in s each letter of q, in lower case, matches
// for its best score, or nil when s lacks them, so a list can mark them
// (B152).
func Positions(q, s string) []int {
	if q == "" {
		return nil
	}
	if _, ok := fuzzy(q, s, new([]int32)); !ok {
		return nil
	}
	n := len(s)
	bonus := make([]int32, n)
	bonuses(s, 0, bonus)
	d, m := make([][]int32, len(q)), make([][]int32, len(q))
	for i := range len(q) {
		d[i], m[i] = make([]int32, n), make([]int32, n)
		var pd, pm []int32
		if i > 0 {
			pd, pm = d[i-1], m[i-1]
		}
		row(q, i, s, 0, bonus, pd, pm, d[i], m[i])
	}
	// Back from the end, each letter takes the last place that leads to the
	// best score, or the place a run of letters needs.
	out := make([]int, len(q))
	run := false
	j := n - 1
	for i := len(q) - 1; i >= 0; i-- {
		for ; j >= 0; j-- {
			if d[i][j] > none/2 && (run || d[i][j] == m[i][j]) {
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

// bonuses fills bonus with what a match at each byte of s from start
// scores.
func bonuses(s string, start int, bonus []int32) {
	for j := range bonus {
		bonus[j] = bonusAt(s, start+j)
	}
}

// bonusAt is what a match at s[j] scores for the byte before it: after a
// separator, or a capital after a lower case letter.
func bonusAt(s string, j int) int32 {
	prev, c := byte('/'), s[j]
	if j > 0 {
		prev = s[j-1]
	}
	switch {
	case prev == '/' || prev == os.PathSeparator:
		return matchSlash
	case prev == '-' || prev == '_' || prev == ' ':
		return matchWord
	case prev == '.':
		return matchDot
	case prev >= 'a' && prev <= 'z' && c >= 'A' && c <= 'Z':
		return matchCapital
	}
	return 0
}

// row fills d and m for q's letter i from pd and pm, the row of q[i-1]: d
// is the best score with q[i] matched at s[j], and m the best with
// q[:i+1] matched by s[:j+1]. s starts at byte off of the text matched.
func row(q string, i int, s string, off int, bonus, pd, pm, d, m []int32) {
	best := int32(none)
	gap := int32(gapInner)
	if i == len(q)-1 {
		gap = gapTrailing
	}
	for j := range len(s) {
		switch {
		case q[i] != lowerByte(s[j]):
			d[j] = none
		case i == 0:
			d[j] = int32(off+j)*gapLeading + bonus[j]
		case j > 0:
			d[j] = max(pm[j-1]+bonus[j], pd[j-1]+matchRun)
		default:
			d[j] = none
		}
		best = max(d[j], best+gap, none)
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
