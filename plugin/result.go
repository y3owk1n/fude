package plugin

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"

	"github.com/tetratelabs/wazero/api"

	fude "github.com/y3owk1n/fude/api"
)

// needs is the capability each field of a Result needs, "" for none. A
// plugin's result may ask for what a module's may, within its capabilities
// (D51).
var needs = map[string]string{
	"Edits": "buffer.write", "Travel": "buffer.write",
	"Decorate": "ui", "Undecorate": "ui", "Status": "ui", "StatusLeft": "ui", "SessionLeft": "ui", "Capture": "ui",
	"Message": "ui", "Float": "ui", "Show": "ui", "Jump": "ui", "Pick": "ui", "Menu": "ui",
	"Places": "ui", "Complete": "ui", "Cmdline": "ui", "Confirm": "ui", "Moved": "ui",
	"Completion": "ui", "Copy": "ui", "Pins": "ui", "Guides": "ui", "Window": "ui", "Tabs": "ui", "Follow": "ui", "Kick": "ui",
	"Close": "ui", "Drop": "buffer.write", "Windows": "ui", "Scratch": "ui", "Write": "buffer.write",
	"Marks": "ui", "Registers": "ui", "Search": "ui", "Ex": "keys", "Place": "keys", "Each": "keys", "Styles": "ui",
	"Emit": "", "Timers": "", "Set": "", "Log": "", "Selected": "", "Error": "",
	"Jobs": "", "Send": "process", "Terminal": "process", "Build": "process",
	"Requests": "lsp", "Notify": "lsp", "Answers": "lsp", "Apply": "lsp", "Servers": "lsp",
	"Restore": "fs.read", "Keys": "keys",
}

// kinds is the capability each kind of registration needs, "" for none.
// A Typed key hears each key typed before the key does its own work, and a
// Completer hears each word character typed. So they need buffer.read, and
// so does a Write, which reads what it writes.
var kinds = map[fude.Kind]string{
	fude.Command: "", fude.Operator: "", fude.Object: "", fude.Key: "", fude.Insert: "", fude.Plug: "", fude.Source: "",
	fude.Typed: "buffer.read", fude.Completer: "buffer.read", fude.Write: "buffer.read", fude.Page: "", fude.Option: "",
}

// init panics on a Result field with no entry in needs, or a kind with no
// entry in kinds, which no plugin could set or register.
func init() {
	for k := fude.Command; k <= fude.Option; k++ {
		if _, ok := kinds[k]; !ok {
			panic(fmt.Sprintf("plugin: no capability for kind %d", k))
		}
	}
	t := reflect.TypeFor[fude.Result]()
	for i := range t.NumField() {
		if _, ok := needs[t.Field(i).Name]; !ok {
			panic("plugin: no capability for Result." + t.Field(i).Name)
		}
	}
}

// register records, during init, the api.Registration at (ptr, n) as JSON,
// which call runs by its ID.
func (p *Plugin) register(_ context.Context, m api.Module, ptr, n uint32) int32 {
	if p.call == nil || p.call.name != "init" {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	var r fude.Registration
	if !ok || json.Unmarshal(b, &r) != nil || r.Kind < fude.Command || r.Kind > fude.Option || r.Name == "" {
		return errRange
	}
	if c := kinds[r.Kind]; c != "" && !p.allowed(c, "register "+r.Name) {
		return errDenied
	}
	p.registered = append(p.registered, r)
	return 0
}

// result adds the api.Result at (ptr, n), as JSON, to what the call asks
// for, or none of it when it asks for what the plugin may not have.
func (p *Plugin) result(_ context.Context, m api.Module, ptr, n uint32) int32 {
	if p.call == nil {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	var r fude.Result
	if !ok || json.Unmarshal(b, &r) != nil {
		return errRange
	}
	if code := p.check(&r); code != 0 {
		return code
	}
	add(&p.call.Result, r)
	return 0
}

// check returns why the plugin may not ask for r, or 0. It makes a Jump's
// path absolute and puts the plugin's name before each Emit.
func (p *Plugin) check(r *fude.Result) int32 {
	v := reflect.ValueOf(*r)
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		if c := needs[name]; c != "" && !v.Field(i).IsZero() && !p.allowed(c, name) {
			return errDenied
		}
	}
	// fude sets an option other than a buffer's own as a typed :set does,
	// so it needs keys.
	for name := range r.Set {
		if name != "expand_tab" && name != "indent_width" && name != "tab_width" && !p.allowed("keys", "Set "+name) {
			return errDenied
		}
	}
	for _, e := range r.Edits {
		if t, ok := p.textOfBuffer(e.Buffer); !ok || !within(t, e.Start, e.End) {
			return errRange
		}
	}
	for _, d := range r.Decorate {
		if t, ok := p.textOfBuffer(d.Buffer); !ok || !within(t, d.Start, d.End) {
			return errRange
		}
		if code := p.image(d.Image); code != 0 {
			return code
		}
	}
	for _, rq := range r.Requests {
		if rq.Method == "" || rq.Pos > p.call.Text.Len() || rq.End > p.call.Text.Len() {
			return errRange
		}
	}
	for _, j := range r.Jobs {
		switch {
		case j.URL != "" && !j.Stream:
			if !p.allowed("net", "Jobs") {
				return errDenied
			}
		case len(j.Argv) == 0:
			return errRange
		case !p.allowed("process", "Jobs"):
			return errDenied
		case !slices.Contains(p.Programs, j.Argv[0]):
			p.log(fmt.Sprintf("%s: start %s, which it did not declare", p.name, j.Argv[0]))
			return errRun
		}
	}
	// A terminal's command runs through the shell, so the shell is the
	// program it runs.
	if r.Terminal != nil {
		if shell := shellName(); !slices.Contains(p.Programs, shell) {
			p.log(fmt.Sprintf("%s: a terminal runs %s, which it did not declare", p.name, shell))
			return errRun
		}
	}
	if r.Jump != nil && r.Jump.Path != "" {
		path, err := p.inside(r.Jump.Path)
		if err != nil {
			p.log(fmt.Sprintf("%s: open: %v", p.name, err))
			return errFile
		}
		r.Jump.Path = path
	}
	// A plugin's events are its own, so fude names them after it.
	for i, name := range r.Emit {
		r.Emit[i] = p.name + "." + name
	}
	return 0
}

// image returns why the plugin may not show the image at path, or 0: a file
// outside the project, or an address on a host it did not declare.
func (p *Plugin) image(path string) int32 {
	if path == "" {
		return 0
	}
	if u, err := url.Parse(path); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		if !p.allowed("net", "Image") || !slices.Contains(p.Hosts, u.Hostname()) {
			return errNet
		}
		return 0
	}
	if !p.allowed("fs.read", "Image") {
		return errDenied
	}
	if _, err := p.inside(path); err != nil {
		return errFile
	}
	return 0
}

// textOfBuffer is the text of the buffer id, or of the one the call is
// about for 0, or false for no open buffer of that id.
func (p *Plugin) textOfBuffer(id int) (fude.Text, bool) {
	if id == 0 {
		return p.call.Text, true
	}
	o, ok := p.call.Buffers[id]
	return o.Text, ok
}

// within reports whether [start, end) is in t.
func within(t fude.Text, start, end int) bool {
	return start >= 0 && start <= end && end <= t.Len()
}

// add adds what r asks for to what dst asks for: its lists and maps to
// theirs, and each other field it sets in place of dst's.
func add(dst *fude.Result, r fude.Result) {
	d, v := reflect.ValueOf(dst).Elem(), reflect.ValueOf(r)
	for i := range v.NumField() {
		f, to := v.Field(i), d.Field(i)
		switch {
		case f.IsZero():
		case f.Kind() == reflect.Slice:
			to.Set(reflect.AppendSlice(to, f))
		case f.Kind() == reflect.Map:
			if to.IsNil() {
				to.Set(reflect.MakeMap(f.Type()))
			}
			for it := f.MapRange(); it.Next(); {
				to.SetMapIndex(it.Key(), it.Value())
			}
		default:
			to.Set(f)
		}
	}
}

// shellName is the name of the program a terminal runs, which fude's
// internal/shell picks the same way: SHELL, or else sh, and on Windows SHELL
// when Windows can run it, or else pwsh, or else powershell.
func shellName() string {
	if runtime.GOOS != "windows" {
		return filepath.Base(cmp.Or(os.Getenv("SHELL"), "/bin/sh"))
	}
	for _, name := range []string{os.Getenv("SHELL"), "pwsh", "powershell"} {
		if path, err := exec.LookPath(name); name != "" && err == nil {
			return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}
	return "powershell"
}
