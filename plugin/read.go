package plugin

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/tetratelabs/wazero/api"

	fude "github.com/y3owk1n/fude/api"
)

// reads is the capability each field of a Scope needs, "" for none. A
// plugin reads any field a module's scope has, within its capabilities. It
// reads Text through text and the other text functions, and never Free
// (D51).
var reads = map[string]string{
	"About": "", "Option": "", "Config": "", "Keys": "", "Commands": "", "HelpTags": "",
	"HelpAt": "", "Grammars": "", "Sessions": "",
	"Buffers": "buffer.read", "Language": "buffer.read", "Spans": "buffer.read", "Query": "buffer.read",
	"Comment": "buffer.read", "Object": "buffer.read", "Node": "buffer.read", "States": "buffer.read",
	"StateText": "buffer.read", "Marks": "buffer.read", "Jumps": "buffer.read", "Changes": "buffer.read",
	"Register": "buffer.read", "Entries": "buffer.read",
	"Decorations": "ui", "Hints": "ui", "Window": "ui", "Messages": "ui", "Session": "ui",
	"Servers": "lsp",
}

// init panics on a Scope field with no entry in reads, which no plugin
// could read.
func init() {
	t := reflect.TypeFor[fude.Scope]()
	for i := range t.NumField() {
		name := t.Field(i).Name
		if _, ok := reads[name]; !ok && name != "Text" && name != "Free" {
			panic("plugin: no capability for Scope." + name)
		}
	}
}

// readScope reads the field of the call's Scope that the JSON {"Name":
// name, "Args": [...]} at (ptr, n) names, and answers with a JSON list.
// For a func the list holds what it returns when called with Args, an
// error as its text, and for any other field the field. It returns
// errRange for a name the scope does not have, or for arguments the func
// does not take or fails on.
func (p *Plugin) readScope(ctx context.Context, m api.Module, ptr, n uint32) (code int64) {
	if p.call == nil {
		return errDenied
	}
	b, ok := m.Memory().Read(ptr, n)
	var q struct {
		Name string
		Args []json.RawMessage
	}
	if !ok || json.Unmarshal(b, &q) != nil {
		return errRange
	}
	c, known := reads[q.Name]
	switch {
	case !known:
		return errRange
	case c != "" && !p.allowed(c, q.Name):
		return errDenied
	}
	f := reflect.ValueOf(p.call.Scope).FieldByName(q.Name)
	out := []any{f.Interface()}
	if f.Kind() == reflect.Func {
		if f.IsNil() || f.Type().NumIn() != len(q.Args) {
			return errRange
		}
		in := make([]reflect.Value, len(q.Args))
		for i, a := range q.Args {
			v := reflect.New(f.Type().In(i))
			if json.Unmarshal(a, v.Interface()) != nil {
				return errRange
			}
			in[i] = v.Elem()
		}
		// A func that fails on its arguments, such as a place past the
		// text's end, answers none.
		defer func() {
			if recover() != nil {
				code = errRange
			}
		}()
		out = out[:0]
		for _, r := range f.Call(in) {
			if r.Type() == reflect.TypeFor[error]() {
				err, _ := r.Interface().(error)
				out = append(out, errText(err))
				continue
			}
			out = append(out, r.Interface())
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return errRange
	}
	return give(ctx, m, string(data))
}

// errText is err's text, or "" for none.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
