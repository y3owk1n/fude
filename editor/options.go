package editor

import (
	"slices"
	"strings"
)

// switches are the options :set turns on and off, by their names in
// config.toml.
func (o *options) switches() map[string]*bool {
	return map[string]*bool{"number": &o.Number, "relative_number": &o.RelativeNumber}
}

// set runs :set on its arguments: name turns an option on, noname off,
// name! the other way, and name? shows it. With no arguments it shows every
// option. It changes the current window and the default for the session's
// windows opened later (B39).
func (e *Session) set(args string) {
	opts := e.win.opts.switches()
	var shown []string
	if args == "" {
		for name := range opts {
			shown = append(shown, name)
		}
		slices.Sort(shown)
		args = strings.Join(shown, "? ") + "?"
		shown = nil
	}
	for _, arg := range strings.Fields(args) {
		name := strings.TrimRight(arg, "?!")
		on, ok := opts[name]
		value := true
		if !ok {
			if on, ok = opts[strings.TrimPrefix(name, "no")]; !ok {
				e.message = "no option named " + name
				return
			}
			name, value = strings.TrimPrefix(name, "no"), false
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
			*on = value
		}
	}
	e.opts = e.win.opts
	e.message = strings.Join(shown, "  ")
}
