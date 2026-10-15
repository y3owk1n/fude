package editor

import (
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
					e.message = fmt.Sprintf("%s takes a number of 0 or more, not %q", name, value)
					return
				}
				*n = v
			case strings.HasSuffix(arg, "?") || !isNumber:
				shown = append(shown, fmt.Sprintf("%s=%d", name, *n))
			}
			continue
		}
		on, ok := switches[name]
		turn := true
		if !ok {
			if on, ok = switches[strings.TrimPrefix(name, "no")]; !ok || isNumber {
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
	e.opts = e.win.opts
	e.message = strings.Join(shown, "  ")
}
