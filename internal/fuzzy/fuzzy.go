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
	"sync/atomic"
)

// Match returns the items of list query matches, the best first, and all
// of them for no query. A tie keeps the order of list.
func Match(list []string, query string) []string {
	r, _ := All(list).Narrow(query, nil)
	out := make([]string, r.Len())
	for i := range out {
		out[i] = r.At(i)
	}
	return out
}

// Ranked is the items of a list a query matched, the best first. A tie
// keeps the order of the list.
type Ranked struct {
	list []string
	in   []int32 // the matches' places in list, in its order, nil for all of it
	runs [][]hit // each core's matches, the best first, nil for no query
	best []int32 // the first matches, the best first, merged from runs as At reads
}

// hit is the item at a place in a list, and its score.
type hit struct{ at, score int32 }

// All is every item of list, in order, as no query ranks them.
func All(list []string) *Ranked { return &Ranked{list: list} }

// Len is how many items matched.
func (r *Ranked) Len() int {
	switch {
	case r == nil:
		return 0
	case r.in == nil:
		return len(r.list)
	}
	return len(r.in)
}

// At returns the match ranked i.
func (r *Ranked) At(i int) string {
	if r.runs == nil {
		if r.in == nil {
			return r.list[i]
		}
		return r.list[r.in[i]]
	}
	for len(r.best) <= i {
		w := -1
		for k, run := range r.runs {
			if len(run) > 0 && (w < 0 || better(run[0], r.runs[w][0]) < 0) {
				w = k
			}
		}
		r.best = append(r.best, r.runs[w][0].at)
		r.runs[w] = r.runs[w][1:]
	}
	return r.list[r.best[i]]
}

// Narrow ranks those of r's matches that query matches. A query that grows
// r's matches nothing else. Narrow gives up, and reports false, once stop
// is set.
func (r *Ranked) Narrow(query string, stop *atomic.Bool) (*Ranked, bool) {
	if query == "" {
		return &Ranked{list: r.list, in: r.in}, true
	}
	q, n := Lower(query), r.Len()
	workers := 1
	if n >= parallelFrom {
		workers = runtime.GOMAXPROCS(0)
	}
	// Each core scores and sorts a run of matches, which At merges.
	size := (n + workers - 1) / workers
	runs := make([][]hit, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			var scratch []int32
			for k := w * size; k < min((w+1)*size, n); k++ {
				if k%1024 == 0 && stop != nil && stop.Load() {
					return
				}
				at := int32(k)
				if r.in != nil {
					at = r.in[k]
				}
				if s, ok := fuzzy(q, r.list[at], &scratch); ok {
					runs[w] = append(runs[w], hit{at, s})
				}
			}
		})
	}
	wg.Wait()
	if stop != nil && stop.Load() {
		return nil, false
	}
	total := 0
	for _, run := range runs {
		total += len(run)
	}
	// The runs are in the list's order until they are sorted.
	in := make([]int32, 0, total)
	for _, run := range runs {
		for _, h := range run {
			in = append(in, h.at)
		}
	}
	for _, run := range runs {
		wg.Go(func() { slices.SortFunc(run, better) })
	}
	wg.Wait()
	return &Ranked{list: r.list, in: in, runs: runs}, true
}

// parallelFrom is the fewest items Narrow splits across cores. Fewer, such
// as a command line's words, match faster on one core.
const parallelFrom = 8192

// better orders a before b when it scores more, or ties and comes first.
func better(a, b hit) int {
	switch {
	case a.score > b.score:
		return -1
	case a.score < b.score:
		return 1
	}
	return int(a.at) - int(b.at)
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
	if cap(*scratch) < 2*len(q) {
		*scratch = make([]int32, 2*len(q))
	}
	// Letter i takes a place from lo[i], where the letters before it first
	// fit, to hi[i], where the letters after it last fit.
	lo, hi := (*scratch)[:len(q)], (*scratch)[len(q):2*len(q)]
	at := 0
	for i := range len(q) {
		k := indexFold(s[at:], q[i])
		if k < 0 {
			return 0, false
		}
		lo[i] = int32(at + k)
		at += k + 1
	}
	if len(q) == len(s) {
		return math.MaxInt32, true
	}
	at = len(s)
	for i := len(q) - 1; i >= 0; i-- {
		for at--; lowerByte(s[at]) != q[i]; at-- {
		}
		hi[i] = int32(at)
	}
	// Each byte after the last letter's last place costs a trailing gap.
	start, end := int(lo[0]), int(hi[len(q)-1])
	var score int32
	if len(q) == 1 {
		score = single(q[0], s, start, end)
	} else {
		n := end - start + 1
		if cap(*scratch) < 2*len(q)+4*n {
			*scratch = make([]int32, 2*len(q)+4*n)
		}
		buf := (*scratch)[2*len(q) : 2*len(q)+4*n]
		d, m, pd, pm := buf[:n], buf[n:2*n], buf[2*n:3*n], buf[3*n:]
		for i := range len(q) {
			// Row i+1 reads this row only before a place of its letter, and
			// so only before hi[i+1].
			to := end + 1
			if i < len(q)-1 {
				to = int(hi[i+1])
			}
			row(q, i, s, start, int(lo[i]), to, pd, pm, d, m)
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
	d, m := make([][]int32, len(q)), make([][]int32, len(q))
	for i := range len(q) {
		d[i], m[i] = make([]int32, n), make([]int32, n)
		var pd, pm []int32
		if i > 0 {
			pd, pm = d[i-1], m[i-1]
		}
		row(q, i, s, 0, 0, n, pd, pm, d[i], m[i])
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

// row fills d and m from s[from] to s[to-1] for q's letter i from pd and
// pm, the row of q[i-1]: d is the best score with q[i] matched at s[j], and
// m the best with q[:i+1] matched by s[:j+1]. The rows start at s[off].
func row(q string, i int, s string, off, from, to int, pd, pm, d, m []int32) {
	best := int32(none)
	gap := int32(gapInner)
	if i == len(q)-1 {
		gap = gapTrailing
	}
	for j := from; j < to; j++ {
		k := j - off
		switch {
		case q[i] != lowerByte(s[j]):
			d[k] = none
		case i == 0:
			d[k] = int32(j)*gapLeading + bonusAt(s, j)
		case k > 0:
			d[k] = max(pm[k-1]+bonusAt(s, j), pd[k-1]+matchRun)
		default:
			d[k] = none
		}
		best = max(d[k], best+gap, none)
		m[k] = best
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
