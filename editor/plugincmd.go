package editor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/install"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/plugin"
)

// pluginState is the plugin of a [[plugin]] table as :plugin lists it (B599).
type pluginState struct {
	name  string         // its manifest's once it loads, else its repo's or file's
	p     *plugin.Plugin // nil until it loads
	state string         // loading, installing, building, not installed, disabled, or failed: why
}

// pluginCommand runs :plugin, which lists the plugins, or with arg
// installs, updates, enables, disables or reloads them.
func (e *Session) pluginCommand(arg string) error {
	verb, name, _ := strings.Cut(strings.TrimSpace(arg), " ")
	name = strings.TrimSpace(name)
	switch {
	case verb == "":
		e.listPlugins()
		return nil
	case verb == "reload" && name == "":
		return e.reloadPlugins()
	case verb == "install":
		return e.installPlugins(name)
	case verb == "update":
		return e.updatePlugins(name)
	case (verb == "enable" || verb == "disable") && name != "":
		return e.choosePlugin(name, verb == "enable")
	}
	return errors.New("plugin what? install, update, enable {name}, disable {name} or reload")
}

// pluginWords are what :plugin takes, with the plugins' names.
func (e *Session) pluginWords() []string {
	words := []string{"install", "update", "reload"}
	for _, s := range e.cfg.Plugin {
		name := e.plugName(s)
		words = append(words, "enable "+name, "disable "+name)
		if s.Repo != "" {
			words = append(words, "install "+name, "update "+name)
		}
	}
	return words
}

// plugName is the name :plugin knows the table s by.
func (e *Session) plugName(s pluginConfig) string {
	if pl := e.plugs[s.source()]; pl != nil && pl.name != "" {
		return pl.name
	}
	if s.Repo != "" {
		return install.Name(s.Repo)
	}
	return strings.TrimSuffix(filepath.Base(s.Path), ".wasm")
}

// listPlugins shows each table's plugin, where it comes from and whether
// it loaded, with the capabilities it asks for that its table does not
// grant (B599, B604).
func (e *Session) listPlugins() {
	if len(e.cfg.Plugin) == 0 {
		e.message = "config.toml lists no plugins"
		return
	}
	lock, err := install.Read()
	if err != nil {
		e.message = err.Error()
	}
	lines := make([]string, len(e.cfg.Plugin))
	for i, s := range e.cfg.Plugin {
		from := s.Path
		if s.Repo != "" {
			from = strings.TrimSpace(s.Repo + " " + s.Version)
			if l, ok := lock.Find(s.Repo); ok {
				from = s.Repo + " " + install.Version(l)
			}
		}
		state := "loading"
		switch pl := e.plugs[s.source()]; {
		case pl == nil:
		case pl.p != nil && !slices.Contains(e.modules, api.Module(pl.p)):
			state = "unloaded"
		case pl.p != nil:
			state = "loaded"
			if wants := missing(pl.p.Manifest.Capabilities, s.Capabilities); len(wants) > 0 {
				state += ", asks for " + strings.Join(wants, ", ") + ", which its table does not grant"
			}
		default:
			state = pl.state
		}
		lines[i] = fmt.Sprintf("%-16s %-48s %s", e.plugName(s), from, state)
	}
	e.panel(nil, api.Panel{Unnumbered: true, Title: "[plugins]", Text: strings.Join(lines, "\n")})
}

// missing returns what want lists that have does not.
func missing(want, have []string) []string {
	var out []string
	for _, w := range want {
		if !slices.Contains(have, w) {
			out = append(out, w)
		}
	}
	return out
}

// pluginTable returns the [[plugin]] table whose plugin is called name, and
// which it is.
func (e *Session) pluginTable(name string) (int, pluginConfig, bool) {
	i := slices.IndexFunc(e.cfg.Plugin, func(s pluginConfig) bool { return e.plugName(s) == name })
	if i < 0 {
		return 0, pluginConfig{}, false
	}
	return i, e.cfg.Plugin[i], true
}

// unplug unloads src's plugin, if it is loaded, and takes out what it
// added.
func (e *Session) unplug(src string) {
	if pl := e.plugs[src]; pl != nil && pl.p != nil && slices.Contains(e.modules, api.Module(pl.p)) {
		e.unregister(pl.p)
		e.drop(pl.p)
	}
}

// installPlugins installs each repo's plugin that is not installed, or
// the one called name again, even after it failed (B599).
func (e *Session) installPlugins(name string) error {
	lock, err := install.Read()
	if err != nil {
		return err
	}
	n := 0
	for i, s := range e.cfg.Plugin {
		pl := e.plugs[s.source()]
		switch {
		case s.Repo == "" || name != "" && e.plugName(s) != name:
			continue
		case name == "" && pl != nil && pl.state != "not installed" && !strings.HasPrefix(pl.state, "failed"):
			continue
		}
		if name != "" {
			// Its file goes, so it installs anew at the commit locked.
			if l, ok := lock.Find(s.Repo); ok {
				p, err := install.File(l)
				if err == nil {
					err = os.Remove(p)
				}
				if err != nil && !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("install %s again: %w", name, err)
				}
			}
		}
		e.unplug(s.source())
		e.loadPlugin(i, s, lock, true)
		n++
	}
	switch {
	case n == 0 && name != "":
		return errors.New("no plugin of a repo is called " + name)
	case n == 0:
		e.message = "every plugin of a repo is installed"
	}
	return nil
}

// updatePlugins moves each repo's plugin, or the one called name, to the
// newest release fude loads, or a branch to its newest commit, and loads
// it anew. A plugin pinned to a release or a commit stays, and so does
// one whose newer version asks for a capability its table does not grant
// (B599).
func (e *Session) updatePlugins(name string) error {
	lock, err := install.Read()
	if err != nil {
		return err
	}
	type job struct {
		i      int
		s      pluginConfig
		locked install.Entry
	}
	var jobs []job
	var notes []string
	for i, s := range e.cfg.Plugin {
		switch {
		case s.Repo == "" || name != "" && e.plugName(s) != name:
		case install.Pinned(s.Version):
			notes = append(notes, e.plugName(s)+" is pinned to "+s.Version)
		default:
			l, _ := lock.Find(s.Repo)
			jobs = append(jobs, job{i, s, l})
		}
	}
	switch {
	case name != "" && len(jobs)+len(notes) == 0:
		return errors.New("no plugin of a repo is called " + name)
	case len(jobs) == 0:
		e.message = cmp.Or(strings.Join(notes, ", "), "config.toml lists no plugin of a repo")
		return nil
	}
	e.message = fmt.Sprintf("updating %d plugins", len(jobs))
	if len(jobs) == 1 {
		e.message = "updating " + e.plugName(jobs[0].s)
	}
	e.working++
	go func() {
		type done struct {
			job
			entry install.Entry
		}
		var updated []done
		for _, j := range jobs {
			at := time.Now()
			entry, m, err := install.Update(context.Background(), j.s.Repo, j.s.Version, j.s.Capabilities, j.locked, e.version)
			trace.Since("plugin", "update "+j.s.Repo, at, err)
			switch {
			case err != nil:
				notes = append(notes, err.Error())
			case entry.Commit != j.locked.Commit:
				notes = append(notes, install.Updated(m.Name, j.locked, entry))
				updated = append(updated, done{j, entry})
			}
		}
		e.queue <- func() {
			e.working--
			for _, d := range updated {
				if err := e.lock(d.entry); err != nil {
					notes = append(notes, err.Error())
				}
			}
			lock, err := install.Read()
			if err != nil {
				notes = append(notes, err.Error())
			}
			for _, d := range updated {
				e.unplug(d.s.source())
				e.loadPlugin(d.i, d.s, lock, false)
			}
			e.message = cmp.Or(strings.Join(notes, ", "), "every plugin is up to date")
		}
	}()
	return nil
}

// choosePlugin enables or disables the plugin called name until fude
// quits. Enabling loads it, a table's with disabled = true too, and
// disabling unloads it with what it added (B600).
func (e *Session) choosePlugin(name string, on bool) error {
	i, s, ok := e.pluginTable(name)
	if !ok {
		return errors.New("no plugin is called " + name)
	}
	src := s.source()
	e.chosen[src] = on
	if !on {
		e.unplug(src)
		e.plugs[src] = &pluginState{name: name, state: "disabled"}
		e.message = "disabled " + name
		return nil
	}
	if pl := e.plugs[src]; pl != nil && pl.p != nil && slices.Contains(e.modules, api.Module(pl.p)) {
		e.message = name + " is already enabled"
		return nil
	}
	lock, err := install.Read()
	if err != nil {
		return err
	}
	e.loadPlugin(i, s, lock, false)
	e.message = "enabled " + name
	return nil
}
