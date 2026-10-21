package vim

import (
	"cmp"
	"slices"
	"strings"
)

// Binding is keys the machine knows, as help shows them. Kind is the mode
// they work in, or motion, operator or object for the keys that work in
// several.
type Binding struct {
	Kind, Keys, Help string
}

// Bindings returns every key the machine knows, by kind and then by keys.
// It does not hold the host's mappings.
func Bindings() []Binding {
	var out []Binding
	add := func(kind, keys, help string) { out = append(out, Binding{kind, keys, help}) }
	for keys, mk := range motions {
		add("motion", keys, mk.help)
	}
	for keys, help := range operators {
		add("operator", keys+"{motion}", help+". Typed twice, it acts on the line")
	}
	for keys, obj := range textObjects {
		add("object", "i"+keys, obj.inner)
		add("object", "a"+keys, obj.around)
	}
	for keys, a := range aliases {
		add("normal", keys, a.help)
	}
	for keys, ek := range edits {
		add("normal", keys, ek.help)
	}
	for keys, nk := range normals {
		add("normal", keysText(Keys(keys)), nk.help)
	}
	for keys, sk := range scrolls {
		add("normal", keys, sk.help)
	}
	for keys, sk := range sideways {
		add("normal", keys, sk.help)
	}
	for k, to := range arrows {
		add("normal", string(k), "does what "+string(to)+" does")
	}
	out = append(out, prefixes...)
	for keys, vk := range visuals {
		add("visual", keys, vk.help)
	}
	for keys, op := range visualOps {
		add("visual", keys, op.help)
	}
	for keys, ik := range inserts {
		add("insert", keys, ik.help)
	}
	for keys, ck := range cmdlineKeys {
		add("command line", keys, ck.help)
	}
	slices.SortFunc(out, func(a, b Binding) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(strings.ToLower(a.Keys), strings.ToLower(b.Keys)), strings.Compare(a.Keys, b.Keys))
	})
	return out
}
