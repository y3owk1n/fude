// Package editorconfig sets a file's indentation from its .editorconfig
// files, as editorconfig.org's spec says (B238).
package editorconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/trace"
)

// Module is editorconfig.
type Module struct{}

// New returns editorconfig.
func New() Module { return Module{} }

func (Module) Name() string                   { return "editorconfig" }
func (Module) Subscribed() []string           { return []string{"buffer.opened"} }
func (Module) Registered() []api.Registration { return nil }

func (Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event sets the options a file's .editorconfig files give it as it opens,
// unless editorconfig is off in config.toml.
func (Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	path := s.About["path"]
	if on, _ := s.Option("editorconfig"); on != "true" || path == "" {
		return api.Result{}, nil
	}
	ec, err := readEditorconfig(path)
	if err != nil {
		return api.Result{Error: err.Error()}, nil
	}
	set := map[string]string{}
	if ec.expandTab != nil {
		set["expand_tab"] = strconv.FormatBool(*ec.expandTab)
	}
	if ec.indent != nil {
		set["indent_width"] = strconv.Itoa(*ec.indent)
	}
	if ec.tab != nil {
		set["tab_width"] = strconv.Itoa(*ec.tab)
	}
	return api.Result{Set: set}, nil
}

func (Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}
func (Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (Module) Close(context.Context) error                   { return nil }

// editorconfig is the indentation .editorconfig files set for a file, nil
// where they set none. An indent of 0 is the tab width.
type editorconfig struct {
	expandTab *bool
	indent    *int // 0 for the tab width
	tab       *int
}

// readEditorconfig reads the .editorconfig files from path's folder up to
// the first that sets root = true. A nearer file wins over one above it.
func readEditorconfig(path string) (editorconfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return editorconfig{}, err
	}
	var found []map[string]string // nearest first
	for dir := filepath.Dir(abs); ; {
		name := filepath.Join(dir, ".editorconfig")
		src, err := os.ReadFile(name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return editorconfig{}, fmt.Errorf("read %s: %w", name, err)
		}
		if err == nil {
			trace.Record("file", fmt.Sprintf("read %q, %d bytes", name, len(src)), 0)
			rel, err := filepath.Rel(dir, abs)
			if err != nil {
				return editorconfig{}, err
			}
			pairs, root := ecPairs(string(src), filepath.ToSlash(rel))
			if found = append(found, pairs); root {
				break
			}
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	pairs := map[string]string{}
	for _, p := range slices.Backward(found) {
		maps.Copy(pairs, p)
	}
	return ecFrom(pairs), nil
}

// ecPairs returns the pairs of an .editorconfig's sections that match rel,
// the file's path from its folder, later ones over earlier ones, and
// whether it sets root = true.
func ecPairs(src, rel string) (map[string]string, bool) {
	pairs := map[string]string{}
	root, inSection, match := false, false, false
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[' && strings.HasSuffix(line, "]"):
			inSection, match = true, ecMatch(line[1:len(line)-1], rel)
		default:
			k, v, ok := strings.Cut(line, "=")
			k, v = strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))
			switch {
			case !ok:
			case !inSection && k == "root":
				root = v == "true"
			case match:
				pairs[k] = v
			}
		}
	}
	return pairs, root
}

// numRange is a glob's {n1..n2}.
var numRange = regexp.MustCompile(`^([+-]?\d+)\.\.([+-]?\d+)$`)

// ecMatch reports whether glob, a section's name, matches rel. A glob
// without a slash matches a name in any folder below the file's.
func ecMatch(glob, rel string) bool {
	if strings.HasSuffix(glob, "/") {
		return false
	}
	prefix := "(?:.*/)?"
	if strings.Contains(glob, "/") {
		glob, prefix = strings.TrimPrefix(glob, "/"), ""
	}
	var b strings.Builder
	var ranges [][2]int
	var braces []bool // for each open brace, whether it holds a choice
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case c == '\\' && i+1 < len(glob):
			i++
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		case c == '*' && strings.HasPrefix(glob[i:], "**"):
			i++
			b.WriteString(".*")
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[' && strings.IndexByte(glob[i:], ']') > 1:
			end := i + strings.IndexByte(glob[i:], ']')
			set, not := strings.CutPrefix(glob[i+1:end], "!")
			b.WriteString("[")
			if not {
				b.WriteString("^")
			}
			b.WriteString(strings.NewReplacer(`\`, `\\`, `[`, `\[`, `^`, `\^`).Replace(set))
			b.WriteString("]")
			i = end
		case c == '{':
			end := closeBrace(glob, i)
			if m := numRange.FindStringSubmatch(glob[i+1 : max(end, i+1)]); end > 0 && m != nil {
				lo, _ := strconv.Atoi(m[1])
				hi, _ := strconv.Atoi(m[2])
				ranges = append(ranges, [2]int{lo, hi})
				b.WriteString(`([+-]?\d+)`)
				i = end
				continue
			}
			choice := end > 0 && hasChoice(glob[i+1:end])
			braces = append(braces, choice)
			if choice {
				b.WriteString("(?:")
			} else {
				b.WriteString(`\{`)
			}
		case c == ',' && len(braces) > 0 && braces[len(braces)-1]:
			b.WriteString("|")
		case c == '}' && len(braces) > 0:
			if braces[len(braces)-1] {
				b.WriteString(")")
			} else {
				b.WriteString(`\}`)
			}
			braces = braces[:len(braces)-1]
		default:
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		}
	}
	re, err := regexp.Compile("^" + prefix + b.String() + "$")
	if err != nil {
		return false
	}
	m := re.FindStringSubmatch(rel)
	if m == nil {
		return false
	}
	for i, r := range ranges {
		if n, err := strconv.Atoi(m[i+1]); err != nil || n < r[0] || n > r[1] {
			return false
		}
	}
	return true
}

// closeBrace returns where the brace that opens at glob[open] closes, or -1.
func closeBrace(glob string, open int) int {
	depth := 0
	for i := open; i < len(glob); i++ {
		switch glob[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// hasChoice reports whether s, the inside of a brace, has a comma outside
// the braces it holds.
func hasChoice(s string) bool {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

// ecFrom returns the indentation pairs set. tab_width is indent_size when
// unset, and unset or a value fude does not know sets nothing.
func ecFrom(pairs map[string]string) editorconfig {
	var ec editorconfig
	switch pairs["indent_style"] {
	case "tab":
		ec.expandTab = new(false)
	case "space":
		ec.expandTab = new(true)
	}
	if pairs["indent_size"] == "tab" {
		ec.indent = new(0)
	} else if n, err := strconv.Atoi(pairs["indent_size"]); err == nil && n > 0 {
		ec.indent = &n
	}
	if n, err := strconv.Atoi(pairs["tab_width"]); err == nil && n > 0 {
		ec.tab = &n
	} else if ec.indent != nil && *ec.indent > 0 {
		ec.tab = new(*ec.indent)
	}
	return ec
}
