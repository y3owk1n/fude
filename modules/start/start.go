// Package start shows the start screen of fude started with no file: fude's
// version, what each key does, the recent files, and the working
// directory's session when it has one (B225, B226). :version shows the
// version too (B349).
package start

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/keep"
)

// What start registers, by the ids Call runs them by. The recent files
// follow, one id for each.
const (
	open uint32 = iota + 1
	config
	version
	recentFile
)

// keys are the start screen's keys, which [keymap.start] changes. A digit
// opens that recent file, and <CR> runs the key its line starts with.
var keys = map[string]string{
	"f": ":pick files<CR>", "g": ":pick grep<CR>", "r": ":pick recent<CR>", "e": ":enew<CR>",
	"c": "<Plug>(start.config)", "q": ":qa<CR>", "<CR>": "<Plug>(start.open)", "s": "<Plug>(session.restore)",
	"1": "<Plug>(start.1)", "2": "<Plug>(start.2)", "3": "<Plug>(start.3)", "4": "<Plug>(start.4)", "5": "<Plug>(start.5)",
	"6": "<Plug>(start.6)", "7": "<Plug>(start.7)", "8": "<Plug>(start.8)", "9": "<Plug>(start.9)",
}

// actions are the start screen's first lines, each a key and what it does.
var actions = [][2]string{
	{"f", "find a file"},
	{"g", "search the files' text"},
	{"r", "recent files"},
	{"e", "new buffer"},
	{"c", "open config.toml"},
	{"q", "quit"},
}

// Module is start.
type Module struct{}

// New returns start.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "start" }

func (m *Module) Subscribed() []string { return []string{"editor.started"} }

func (m *Module) Registered() []api.Registration {
	regs := []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "fude started with no file shows the start screen"},
		{Kind: api.Plug, Name: "open", Help: "runs the start screen's line under the cursor", ID: open},
		{Kind: api.Plug, Name: "config", Help: "opens config.toml", ID: config},
		{Kind: api.Command, Name: "version", Help: "shows the version of fude that runs", ID: version},
	}
	for n := 1; n <= 9; n++ {
		regs = append(regs, api.Registration{Kind: api.Plug, Name: strconv.Itoa(n), Help: "opens the start screen's recent file " + strconv.Itoa(n), ID: recentFile + uint32(n-1)})
	}
	return regs
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs the key at the start of the line under the cursor, opens
// config.toml, shows fude's version, or opens a recent file.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	switch id {
	case open:
		t := s.Text
		line := t.LineOf(args[0])
		end := t.Len()
		if line+1 < t.Lines() {
			end = t.LineStart(line+1) - 1
		}
		key, _, _ := strings.Cut(strings.TrimSpace(t.Slice(t.LineStart(line), end)), " ")
		if _, ok := keys[key]; ok {
			return api.Result{Keys: key}, nil
		}
		return api.Result{}, nil
	case config:
		dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
		if err != nil {
			return api.Result{Error: fmt.Sprintf("config.toml: %v", err)}, nil
		}
		// A new machine has no config directory, which :w needs.
		dir = filepath.Join(dir, "fude")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return api.Result{Error: fmt.Sprintf("config.toml: %v", err)}, nil
		}
		return api.Result{Jump: &api.Jump{Path: filepath.Join(dir, "config.toml")}}, nil
	case version:
		return api.Result{Message: "fude " + s.About["version"]}, nil
	}
	// The list is the start screen's, from any window (B225).
	recent, err := recentFiles()
	if err != nil {
		return api.Result{Error: err.Error()}, nil
	}
	if n := int(id - recentFile); n < len(recent) {
		return api.Result{Jump: &api.Jump{Path: recent[n]}}, nil
	}
	return api.Result{Error: fmt.Sprintf("no recent file %d", id-recentFile+1)}, nil
}

// recentFiles are the 9 newest recent files under the working directory,
// or the 9 newest anywhere when it has none.
func recentFiles() ([]string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("recent files: %w", err)
	}
	// The log is for failed writes, and this only reads.
	here, elsewhere, err := keep.NewRecent(keep.RecentPath, func(string) {}).Files("", wd)
	if err != nil {
		return nil, err
	}
	if len(here) == 0 {
		here = elsewhere
	}
	return here[:min(len(here), 9)], nil
}

// Event shows the start screen in place of the empty buffer, with the
// recent files and a line to restore the directory's session first when
// it has one.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	if on, _ := s.Option("start.enabled"); on != "true" {
		return api.Result{}, nil
	}
	var res api.Result
	recent, err := recentFiles()
	if err != nil {
		res.Error = err.Error()
	}
	list := actions
	if path, err := keep.SessionPath(); err == nil {
		if _, err := os.Stat(path); err == nil {
			list = append([][2]string{{"s", "restore the last session here"}}, list...)
		}
	}
	res.Show = screen(s.About["version"], list, recent)
	return res, nil
}

// screen is the start screen: fude's name and version v, a line for each of
// list's keys, and the recent files, numbered.
func screen(v string, list [][2]string, recent []string) *api.Panel {
	var b strings.Builder
	var spans []api.Span
	mark := func(text, style string) {
		spans = append(spans, api.Span{Start: b.Len(), End: b.Len() + len(text), Name: style})
		b.WriteString(text)
	}
	b.WriteString("\n   ")
	mark("fude 筆", "markup.heading")
	if v != "" {
		b.WriteString("  ")
		mark(v, "comment")
	}
	b.WriteString("\n\n")
	cursor := b.Len() + 3
	for _, a := range list {
		b.WriteString("   ")
		mark(a[0], "keyword")
		b.WriteString("  " + a[1] + "\n")
	}
	home, _ := os.UserHomeDir()
	if len(recent) > 0 {
		b.WriteString("\n   ")
		mark("recent files", "markup.heading")
		b.WriteString("\n\n")
		for i, p := range recent {
			b.WriteString("   ")
			mark(strconv.Itoa(i+1), "keyword")
			if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
				p = "~" + p[len(home):]
			}
			dir, name := filepath.Split(p)
			b.WriteString("  " + name)
			if dir != "" {
				b.WriteString("  ")
				mark(strings.TrimSuffix(dir, string(filepath.Separator)), "comment")
			}
			b.WriteString("\n")
		}
	}
	return &api.Panel{Unnumbered: true, Title: "[start]", Text: strings.TrimSuffix(b.String(), "\n"), Spans: spans, Keys: keys, Screen: true, Cursor: cursor}
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
