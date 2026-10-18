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

// set runs :set. With a name and a value it sets the option, as
// config.toml would: true or false for a switch, and a number of 0 or more
// for a number. With a name alone it shows the option, and with neither
// it lists every one in a window. It changes the current window and the
// default for the session's windows opened later, and never config.toml
// (B39).
func (e *Session) set(args string) {
	switches, numbers := e.win.opts.switches(), e.win.opts.numbers()
	name, value, given := strings.Cut(strings.TrimSpace(args), " ")
	value = strings.TrimSpace(value)
	switch _, number := numbers[name]; {
	case name == "":
		var lines []string
		for _, n := range slices.Sorted(maps.Keys(switches)) {
			lines = append(lines, fmt.Sprintf("%-16s %t", n, *switches[n]))
		}
		for _, n := range slices.Sorted(maps.Keys(numbers)) {
			lines = append(lines, fmt.Sprintf("%-16s %d", n, *numbers[n]))
		}
		e.scratch("[set]", strings.Join(lines, "\n"), nil, nil)
		return
	case switches[name] == nil && !number:
		e.message = "no option named " + name
		return
	case !given && number:
		e.message = fmt.Sprintf("%s %d", name, *numbers[name])
		return
	case !given:
		e.message = fmt.Sprintf("%s %t", name, *switches[name])
		return
	}
	was := e.win.opts
	if n, ok := numbers[name]; ok {
		v, err := strconv.Atoi(value)
		if err != nil || v < 0 {
			e.message = fmt.Sprintf("%s takes a number of 0 or more, not %q", name, value)
			return
		}
		*n = v
	} else {
		if value != "true" && value != "false" {
			e.message = fmt.Sprintf("%s takes true or false, not %q", name, value)
			return
		}
		*switches[name] = value == "true"
	}
	e.win.keep(was)
	e.opts = e.win.ownOptions()
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
