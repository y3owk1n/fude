package editor

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/wire"
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
	// The modules hear what changed once it has (B331).
	if given {
		defer e.emit("option.set", name, e.win.buf)
	}
	if _, ok := e.cfg.settings()[name]; ok {
		e.setSetting(name, value, given)
		return
	}
	switch _, number := numbers[name]; {
	case name == "":
		var lines []string
		for _, n := range slices.Sorted(maps.Keys(switches)) {
			lines = append(lines, fmt.Sprintf("%-18s %t", n, *switches[n]))
		}
		for _, n := range slices.Sorted(maps.Keys(numbers)) {
			lines = append(lines, fmt.Sprintf("%-18s %d", n, *numbers[n]))
		}
		settings := e.cfg.settings()
		for _, n := range slices.Sorted(maps.Keys(settings)) {
			lines = append(lines, fmt.Sprintf("%-18s %s", n, settingText(settings[n])))
		}
		e.scratch("[set]", strings.Join(lines, "\n"), e.placement(nil, "", false, "below"))
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
	// A float's options are its own, which new windows do not start with.
	if e.win.anchor == "" {
		e.opts = e.win.ownOptions()
	}
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

// settings are config.toml's settings of the editor, not of a window, that
// :set changes, by their names. They are its switches, numbers, words and
// lists, and the colours of [colors] as colors.<role> (B39, B294).
func (c *config) settings() map[string]reflect.Value {
	out := map[string]reflect.Value{}
	v := reflect.ValueOf(c).Elem()
	for _, f := range reflect.VisibleFields(v.Type()) {
		if f.Anonymous || len(f.Index) > 1 {
			continue
		}
		switch f.Type.Kind() {
		case reflect.Bool, reflect.Uint, reflect.String:
		case reflect.Slice:
			if f.Type.Elem().Kind() != reflect.String {
				continue
			}
		default:
			continue
		}
		out[f.Tag.Get("toml")] = v.FieldByIndex(f.Index)
	}
	colors := reflect.ValueOf(&c.Colors).Elem()
	for _, f := range reflect.VisibleFields(colors.Type()) {
		out["colors."+f.Tag.Get("toml")] = colors.FieldByIndex(f.Index)
	}
	return out
}

// settingText is a setting's value as :set takes it, with a list's items
// apart by commas.
func settingText(v reflect.Value) string {
	switch x := v.Interface().(type) {
	case []string:
		return strings.Join(x, ",")
	case wire.Hex:
		hex, _ := x.MarshalText()
		return string(hex)
	}
	return fmt.Sprint(v.Interface())
}

// setSetting runs :set on a setting of the editor, which every session
// shares, so a guest may not. theme, leader and center act at once, as
// :theme, a new leader and :center would (B39).
func (e *Session) setSetting(name, value string, given bool) {
	v := e.cfg.settings()[name]
	switch {
	case !given && name == "theme":
		// :theme and :center change these too, so they show as they are now.
		e.message = name + " " + e.theme
		return
	case !given && name == "center":
		e.message = fmt.Sprintf("%s %t", name, e.centered)
		return
	case !given:
		e.message = name + " " + settingText(v)
		return
	case e.power != Host:
		e.message = "a guest may not change " + name + ", which every session shares"
		return
	}
	var set reflect.Value
	switch v.Kind() {
	case reflect.Bool:
		if value != "true" && value != "false" {
			e.message = fmt.Sprintf("%s takes true or false, not %q", name, value)
			return
		}
		set = reflect.ValueOf(value == "true")
	case reflect.Uint:
		n, err := strconv.ParseUint(value, 10, 0)
		if err != nil {
			e.message = fmt.Sprintf("%s takes a number of 0 or more, not %q", name, value)
			return
		}
		set = reflect.ValueOf(uint(n))
	case reflect.Slice:
		items := strings.Split(value, ",")
		if bad := slices.IndexFunc(items, func(s string) bool { return !slices.Contains(defaults.Sources, s) }); name == "completion_sources" && bad >= 0 {
			e.message = fmt.Sprintf("%s takes %s, not %q", name, strings.Join(defaults.Sources, ","), items[bad])
			return
		}
		set = reflect.ValueOf(items)
	case reflect.Struct:
		var hex wire.Hex
		if err := hex.UnmarshalText([]byte(value)); err != nil {
			e.message = fmt.Sprintf("%s takes #rrggbb, not %q", name, value)
			return
		}
		set = reflect.ValueOf(hex)
	default:
		set = reflect.ValueOf(value)
	}
	switch name {
	case "theme":
		// config.toml's theme stays, so :theme's <Tab> offers it to switch
		// back to (B158).
		if err := e.setTheme(value); err != nil {
			e.message = err.Error()
		}
		return
	case "leader":
		e.moveLeader(e.cfg.Leader, value)
	case "center":
		if on := value == "true"; on != e.centered {
			e.center()
		}
	case "socket":
		e.message = "socket takes effect when fude starts"
	}
	v.Set(set)
	for _, s := range e.sessions {
		// Windows fit their widths again.
		for _, w := range s.root.windows() {
			w.fitted = nil
		}
		s.layout()
	}
}
