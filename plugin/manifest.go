package plugin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// OldestSDK is the oldest sdk whose plugins fude loads. It moves only with
// a change that breaks the ABI (D77).
const OldestSDK = "v0.3.0"

// Manifest is a plugin's fude.toml, which fude plugin build writes into
// its .wasm with the version of the sdk it built with (B602).
type Manifest struct {
	Name         string   `toml:"name" json:"name"`
	SDK          string   `toml:"-" json:"sdk,omitempty"` // "" for an sdk that is no release
	Capabilities []string `toml:"capabilities" json:"capabilities"`
	Programs     []string `toml:"programs" json:"programs,omitempty"`
	Hosts        []string `toml:"hosts" json:"hosts,omitempty"`
}

// section is the name of the custom section that holds the manifest.
const section = "fude"

var plainName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// readManifest reads the fude.toml in dir and checks it.
func readManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, "fude.toml"))
	if err != nil {
		return m, err
	}
	d := toml.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, fmt.Errorf("fude.toml: %w", err)
	}
	if !plainName.MatchString(m.Name) {
		return m, fmt.Errorf("fude.toml: name %q is not lowercase letters, digits, - and _", m.Name)
	}
	for _, c := range m.Capabilities {
		if !slices.Contains(capabilities, c) {
			return m, fmt.Errorf("fude.toml: no capability %q", c)
		}
	}
	return m, nil
}

// withManifest returns wasm with m in a custom section at its end, which a
// module may have anywhere.
func withManifest(wasm []byte, m Manifest) ([]byte, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	body := binary.AppendUvarint(nil, uint64(len(section)))
	body = append(append(body, section...), payload...)
	out := append(slices.Clip(wasm), 0)
	out = binary.AppendUvarint(out, uint64(len(body)))
	return append(out, body...), nil
}

// ReadManifest returns the manifest in wasm, from the last custom section
// named fude. It walks the sections, so it runs nothing (D77).
func ReadManifest(wasm []byte) (Manifest, error) {
	var m Manifest
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return m, errors.New("not a wasm module")
	}
	var found []byte
	for rest := wasm[8:]; len(rest) > 0; {
		id := rest[0]
		size, n := binary.Uvarint(rest[1:])
		if n <= 0 || uint64(len(rest)-1-n) < size {
			return m, errors.New("a section of the module is cut short")
		}
		body := rest[1+n : 1+n+int(size)]
		rest = rest[1+n+int(size):]
		if id != 0 {
			continue
		}
		l, k := binary.Uvarint(body)
		if k > 0 && uint64(len(body)-k) >= l && string(body[k:k+int(l)]) == section {
			found = body[k+int(l):]
		}
	}
	if found == nil {
		return m, errors.New("it has no manifest, build it with fude plugin build")
	}
	if err := json.Unmarshal(found, &m); err != nil {
		return m, fmt.Errorf("its manifest: %w", err)
	}
	return m, nil
}

// Check returns why fude at version host does not load a plugin of m, or
// nil. host is v1.2.3, or "" for a build that is no release (B603).
func (m Manifest) Check(host string) error {
	switch {
	case m.SDK == "":
		return nil
	case less(m.SDK, OldestSDK):
		return fmt.Errorf("plugin %s was built with sdk %s, and fude takes sdk %s or newer", m.Name, m.SDK, OldestSDK)
	case release.MatchString(host) && less(host, m.SDK):
		return fmt.Errorf("plugin %s needs fude %s or newer, and this is %s", m.Name, m.SDK, host)
	}
	return nil
}

// less reports whether release a, as v1.2.3, comes before b.
func less(a, b string) bool {
	return slices.Compare(parts(a), parts(b)) < 0
}

// parts returns the numbers of a release, as v1.2.3.
func parts(v string) []int {
	var out []int
	for s := range strings.SplitSeq(strings.TrimPrefix(v, "v"), ".") {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out
}
