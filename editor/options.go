package editor

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// switches are the options :set turns on and off, by their names in
// config.toml.
func (o *options) switches() map[string]*bool { return fields[bool](o) }

// numbers are the options :set gives a number.
func (o *options) numbers() map[string]*int { return fields[int](o) }

// fields returns o's fields of type T, by their toml names.
func fields[T any](o *options) map[string]*T {
	out := map[string]*T{}
	v := reflect.ValueOf(o).Elem()
	for _, f := range reflect.VisibleFields(v.Type()) {
		if p, ok := v.FieldByIndex(f.Index).Addr().Interface().(*T); ok {
			out[f.Tag.Get("toml")] = p
		}
	}
	return out
}

// set runs :set on its arguments: name turns a switch on, noname off,
// name! the other way, name=n gives a number, and name? shows an option.
// With no arguments it shows every option. It changes the current window
// and the default for the session's windows opened later (B39).
func (e *Session) set(args string) {
	was := e.win.opts
	switches, numbers := e.win.opts.switches(), e.win.opts.numbers()
	if args == "" {
		names := slices.Sorted(maps.Keys(switches))
		names = append(names, slices.Sorted(maps.Keys(numbers))...)
		args = strings.Join(names, "? ") + "?"
	}
	var shown []string
	for _, arg := range strings.Fields(args) {
		name, value, isNumber := strings.Cut(strings.TrimRight(arg, "?!"), "=")
		if n, ok := numbers[name]; ok {
			switch {
			case isNumber:
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 {
					// A :set with a bad argument changes nothing.
					e.win.opts = was
					e.message = fmt.Sprintf("%s takes a number of 0 or more, not %q", name, value)
					return
				}
				*n = v
			default:
				shown = append(shown, fmt.Sprintf("%s=%d", name, *n))
			}
			continue
		}
		on, ok := switches[name]
		turn := true
		if !ok {
			if on, ok = switches[strings.TrimPrefix(name, "no")]; !ok || isNumber {
				e.win.opts = was
				e.message = "no option named " + name
				return
			}
			name, turn = strings.TrimPrefix(name, "no"), false
		}
		switch {
		case strings.HasSuffix(arg, "?"):
			if !*on {
				name = "no" + name
			}
			shown = append(shown, name)
		case strings.HasSuffix(arg, "!"):
			*on = !*on
		default:
			*on = turn
		}
	}
	e.win.keep(was)
	e.opts = e.win.ownOptions()
	e.message = strings.Join(shown, "  ")
	// A colour column changes how wide centering makes the windows.
	e.layout()
}

// option returns the option or config.toml setting called name there, as
// text. An option comes from opts, which a filetype table may have set, and
// a setting from config.toml. A list or a table is neither (B274).
func option(opts options, cfg *config, name string) (string, bool) {
	for _, v := range []reflect.Value{reflect.ValueOf(opts), reflect.ValueOf(cfg).Elem()} {
		for _, f := range reflect.VisibleFields(v.Type()) {
			if f.Tag.Get("toml") != name {
				continue
			}
			switch fv := v.FieldByIndex(f.Index); fv.Kind() {
			case reflect.Bool, reflect.Int, reflect.Uint, reflect.String:
				return fmt.Sprint(fv), true
			}
		}
	}
	return "", false
}

// configNode returns the value at a dotted path of config.toml, such as
// filetype.go.format, as JSON, or false for none. A table's key follows
// its name in the path (B279).
func configNode(cfg *config, path string) (string, bool) {
	v := reflect.ValueOf(cfg).Elem()
	for _, name := range strings.Split(path, ".") {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return "", false
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Map:
			if v = v.MapIndex(reflect.ValueOf(name)); !v.IsValid() {
				return "", false
			}
		case reflect.Struct:
			i := slices.IndexFunc(reflect.VisibleFields(v.Type()), func(f reflect.StructField) bool { return f.Tag.Get("toml") == name })
			if i < 0 {
				return "", false
			}
			v = v.FieldByIndex(reflect.VisibleFields(v.Type())[i].Index)
		default:
			return "", false
		}
	}
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return "", false
	}
	// The encoder leaves < and > alone, so a key such as <Space> reads as written.
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v.Interface()); err != nil {
		return "", false
	}
	return strings.TrimSuffix(b.String(), "\n"), true
}
