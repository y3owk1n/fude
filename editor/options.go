package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/y3owk1n/fude/api"
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
func (e *Session) set(args string) error {
	switches, numbers := e.win.opts.switches(), e.win.opts.numbers()
	name, value, given := strings.Cut(strings.TrimSpace(args), " ")
	value = strings.TrimSpace(value)
	// The modules hear what changed once it has (B331).
	if given {
		defer e.emit("option.set", name, e.win.buf)
	}
	if name == "filetype" {
		return e.setFiletype(value, given)
	}
	if _, ok := e.cfg.settings()[name]; ok {
		return e.setSetting(name, value, given)
	}
	if _, ok := e.declared[name]; ok {
		return e.setDeclared(name, value, given)
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
		for _, n := range slices.Sorted(maps.Keys(e.declared)) {
			lines = append(lines, fmt.Sprintf("%-18s %s", n, declaredValue(e.win.opts, e.values, n)))
		}
		e.scratch("[set]", strings.Join(lines, "\n"), e.placement(nil, "", false, "below"), false)
		return nil
	case switches[name] == nil && !number:
		return errors.New("no option named " + name)
	case !given && number:
		e.message = fmt.Sprintf("%s %d", name, *numbers[name])
		return nil
	case !given:
		e.message = fmt.Sprintf("%s %t", name, *switches[name])
		return nil
	}
	was := e.win.opts
	if n, ok := numbers[name]; ok {
		v, err := strconv.Atoi(value)
		if err != nil || v < 0 {
			return fmt.Errorf("%s takes a number of 0 or more, not %q", name, value)
		}
		*n = v
	} else {
		if value != "true" && value != "false" {
			return fmt.Errorf("%s takes true or false, not %q", name, value)
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
	return nil
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
	tables := map[string]any{"colors": &c.Colors, "list_chars": &c.ListChars, "fill_chars": &c.FillChars}
	for name, table := range tables {
		t := reflect.ValueOf(table).Elem()
		for _, f := range reflect.VisibleFields(t.Type()) {
			out[name+"."+f.Tag.Get("toml")] = t.FieldByIndex(f.Index)
		}
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
func (e *Session) setSetting(name, value string, given bool) error {
	v := e.cfg.settings()[name]
	switch {
	case !given && name == "theme":
		// :theme and :center change these too, so they show as they are now.
		e.message = name + " " + e.theme
		return nil
	case !given && name == "center":
		e.message = fmt.Sprintf("%s %t", name, e.centered)
		return nil
	case !given:
		e.message = name + " " + settingText(v)
		return nil
	case e.power != Host:
		return errors.New("a guest may not change " + name + ", which every session shares")
	}
	if err := checkSetting(name, value); err != nil {
		return err
	}
	var set reflect.Value
	switch v.Kind() {
	case reflect.Bool:
		if value != "true" && value != "false" {
			return fmt.Errorf("%s takes true or false, not %q", name, value)
		}
		set = reflect.ValueOf(value == "true")
	case reflect.Uint:
		n, err := strconv.ParseUint(value, 10, 0)
		if err != nil {
			return fmt.Errorf("%s takes a number of 0 or more, not %q", name, value)
		}
		set = reflect.ValueOf(uint(n))
	case reflect.Slice:
		items := strings.Split(value, ",")
		if bad := slices.IndexFunc(items, func(s string) bool { return !slices.Contains(defaults.Sources, s) }); name == "completion_sources" && bad >= 0 {
			return fmt.Errorf("%s takes %s, not %q", name, strings.Join(defaults.Sources, ","), items[bad])
		}
		set = reflect.ValueOf(items)
	case reflect.Struct:
		var hex wire.Hex
		if err := hex.UnmarshalText([]byte(value)); err != nil {
			return fmt.Errorf("%s takes #rrggbb, not %q", name, value)
		}
		set = reflect.ValueOf(hex)
	default:
		set = reflect.ValueOf(value)
	}
	switch name {
	case "theme":
		// config.toml's theme stays, so :theme's <Tab> offers it to switch
		// back to (B158).
		return e.setTheme(value)
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
	return nil
}

// checkSetting returns why the setting name may not take value, or nil.
// A character of [list_chars] or [fill_chars] is one column wide, and
// list_chars.tab is two or three of them (B486, B487).
func checkSetting(name, value string) error {
	table, key, _ := strings.Cut(name, ".")
	if table != "list_chars" && table != "fill_chars" || value == "" {
		return nil
	}
	n := utf8.RuneCountInString(value)
	ok, want := n == 1, "one character"
	if key == "tab" {
		ok, want = n == 2 || n == 3, "two or three characters"
	}
	if !ok || displaywidth.String(value) != n {
		return fmt.Errorf("%s takes %s one column wide, not %q", name, want, value)
	}
	return nil
}

// declared is an option a module registered, with the value config.toml or
// its Default starts it at (B377).
type declared struct {
	owner api.Module
	reg   api.Registration
	start string
}

// declare adds the options p registered, starting at the values start
// gives them by name, which are a plugin's options table or a module's
// [module.name] table. A value of the wrong kind leaves the Default, and a
// name that is no option says so (B377).
func (e *Session) declare(p api.Module, start map[string]string) {
	unknown := maps.Clone(start)
	for _, r := range p.Registered() {
		if r.Kind != api.Option {
			continue
		}
		delete(unknown, r.Name)
		name := p.Name() + "." + r.Name
		if _, taken := e.cfg.settings()[name]; taken || e.declared[name].owner != nil {
			e.message = "the option " + name + " is taken"
			continue
		}
		value := r.Default
		if v, ok := start[r.Name]; ok {
			if err := check(name, r.Default, v); err != nil {
				e.message = err.Error()
			} else {
				value = v
			}
		}
		e.declared[name], e.values[name] = declared{p, r, value}, value
	}
	for _, key := range slices.Sorted(maps.Keys(unknown)) {
		e.message = fmt.Sprintf("%s.%s is no option of %s", p.Name(), key, p.Name())
	}
}

// moduleConfig returns what is wrong with cfg's [module] tables: a key that
// names no option of a module fude runs, or a value of the wrong kind
// (B377).
func (e *Session) moduleConfig(cfg config) error {
	var bad []string
	for _, owner := range slices.Sorted(maps.Keys(cfg.Module)) {
		values, err := texts(cfg.Module[owner])
		if err != nil {
			bad = append(bad, fmt.Sprintf("module.%s: %v", owner, err))
			continue
		}
		for _, key := range slices.Sorted(maps.Keys(values)) {
			name := owner + "." + key
			d, ok := e.declared[name]
			if !ok || kind(d.owner) == "plugin" {
				bad = append(bad, "module."+name+" is no option of a module")
			} else if err := check(name, d.reg.Default, values[key]); err != nil {
				bad = append(bad, err.Error())
			}
		}
	}
	if len(bad) > 0 {
		return errors.New(strings.Join(bad, "  "))
	}
	return nil
}

// redeclare starts modules' options at the values cfg's [module] tables
// give them, where those changed. A window option that changed takes its
// new value in every window (B39, B377).
func (e *Session) redeclare(cfg config) {
	for name, d := range e.declared {
		if kind(d.owner) == "plugin" {
			// Its options table starts it again as it loads again.
			continue
		}
		start, _ := texts(cfg.Module[d.owner.Name()])
		value, ok := start[d.reg.Name]
		if !ok {
			value = d.reg.Default
		}
		if value == d.start {
			continue
		}
		d.start = value
		e.declared[name], e.values[name] = d, value
		if !d.reg.Window {
			continue
		}
		for _, s := range e.sessions {
			for _, w := range s.root.windows() {
				w.opts.Modules = with(w.opts.Modules, name, value)
				if w.own != nil {
					w.own.Modules = with(w.own.Modules, name, value)
				}
			}
			s.opts.Modules = with(s.opts.Modules, name, value)
		}
	}
}

// setDeclared runs :set on an option a module registered. A window option
// changes as fude's do, and every session shares another, so a guest may
// not change it (B377).
func (e *Session) setDeclared(name, value string, given bool) error {
	d := e.declared[name]
	switch {
	case !given:
		e.message = name + " " + declaredValue(e.win.opts, e.values, name)
		return nil
	case !d.reg.Window && e.power != Host:
		return errors.New("a guest may not change " + name + ", which every session shares")
	}
	if err := check(name, d.reg.Default, value); err != nil {
		return err
	}
	if !d.reg.Window {
		e.values[name] = value
		return nil
	}
	was := e.win.opts
	e.win.opts.Modules = with(e.win.opts.Modules, name, value)
	e.win.keep(was)
	if e.win.anchor == "" {
		e.opts = e.win.ownOptions()
	}
	return nil
}

// declaredValue returns the value of the module's option name in a window
// with opts, where values has the shared ones (B377).
func declaredValue(opts options, values map[string]string, name string) string {
	if v, ok := opts.Modules[name]; ok {
		return v
	}
	return values[name]
}

// check returns why the option name, whose Default is def, may not take
// value: a switch takes true or false, and a number one of 0 or more.
func check(name, def, value string) error {
	switch {
	case def == "true" || def == "false":
		if value != "true" && value != "false" {
			return fmt.Errorf("%s takes true or false, not %q", name, value)
		}
	case number(def) && !number(value):
		return fmt.Errorf("%s takes a number of 0 or more, not %q", name, value)
	}
	return nil
}

// number reports whether s is a number of 0 or more.
func number(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0
}

// with returns a copy of m with name set to value.
func with(m map[string]string, name, value string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, m)
	out[name] = value
	return out
}
