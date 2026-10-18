package editor

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// disabled is why a server :lsp disable turned off is down. It alone keeps
// the server from starting when a file it serves opens (B160).
const disabled = "disabled"

// notInstalled is why a server whose program is not on PATH is down. It
// keeps the server from starting until :lsp restart or enable.
const notInstalled = "not installed"

// instances are the running servers named name, by root.
func (e *Editor) instances(name string) []*server {
	var out []*server
	for _, s := range e.servers {
		if s.name == name {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b *server) int { return strings.Compare(a.root, b.root) })
	return out
}

// lspCommand runs :lsp sub on the language server name: restart, stop,
// disable to keep it off for every file, or enable. Restart and enable
// attach every buffer it serves that has no server, which also starts one
// that stayed off after crashing (B159, B160).
func (e *Session) lspCommand(sub, name string) {
	switch {
	case name == "":
		e.message = "no language server is set for this file"
		return
	case !slices.Contains([]string{"disable", "enable", "restart", "stop"}, sub):
		e.message = "not an lsp command: " + sub
		return
	}
	e.serversChanged(name)
	if sub != "enable" {
		for _, s := range e.instances(name) {
			s.closing = true
			s.client.Close()
			e.stop(s)
		}
	}
	switch sub {
	case "stop":
		if e.down[name] != disabled {
			e.down[name] = "stopped"
		}
		e.message = "stopped " + name
		return
	case "disable":
		e.down[name] = disabled
	case "enable":
		delete(e.down, name)
	}
	if e.down[name] == notInstalled {
		delete(e.down, name)
	}
	for _, b := range e.bufs {
		if slices.Contains(e.serverNames(b), name) {
			if err := e.attachTo(b, name); err != nil {
				e.message = err.Error()
			}
		}
	}
	e.syncKeys()
	switch {
	case e.down[name] == disabled:
		e.message = name + " is off until :lsp enable"
	case len(e.instances(name)) > 0:
		e.message = name + " is running"
	}
}

// configured returns the filetypes of each language server config.toml
// sets, by its name.
func (e *Editor) configured() map[string][]string {
	out := map[string][]string{}
	for ft, c := range e.cfg.Filetype {
		for _, name := range c.LSP {
			out[name] = append(out[name], ft)
		}
	}
	return out
}

// serverList is the language servers config.toml sets, by name, with their
// state, version, roots and files (B159).
func (e *Editor) serverList() []api.Server {
	configured := e.configured()
	var out []api.Server
	for _, name := range slices.Sorted(maps.Keys(configured)) {
		sv := api.Server{Name: name, State: "no files", Filetypes: slices.Sorted(slices.Values(configured[name]))}
		running := e.instances(name)
		switch down := e.down[name]; {
		case len(running) > 0 && running[0].client.Encoding() == "":
			sv.State = "starting"
		case len(running) > 0:
			sv.State = "running"
		case down == disabled || down == "stopped" || down == notInstalled:
			sv.State = down
		case down != "":
			sv.State, sv.Why = "failed", down
		}
		for _, s := range running {
			sv.Version = cmp.Or(sv.Version, s.client.Version())
			root := api.Root{Path: s.root}
			for _, b := range s.bufs {
				root.Files = append(root.Files, b.file.path)
			}
			sv.Roots = append(sv.Roots, root)
		}
		out = append(out, sv)
	}
	return out
}

// serverActions does what a module asks of language servers, saying what
// each did. A guest may not (B317).
func (e *Session) serverActions(actions []api.ServerAction) {
	if len(actions) == 0 {
		return
	}
	if e.power != Host {
		e.message = "a guest may not " + actions[0].Do + " a language server"
		return
	}
	var said []string
	for _, a := range actions {
		e.lspCommand(a.Do, a.Name)
		said = append(said, e.message)
	}
	e.message = strings.Join(slices.Compact(said), "  ")
}

// serversChanged tells the modules that the state of the language server
// name changed. The modules hear it about the file the current window
// shows, or the one under its float, once the change is done (B317).
func (e *Session) serversChanged(name string) {
	b := e.win.buf
	if e.under != nil {
		b = e.under.buf
	}
	e.emitTo(event{name: "lsp.changed", detail: name, buf: b, from: e})
}
