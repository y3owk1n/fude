package plugin_test

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/plugin"
)

// manifested writes the test plugin name with the manifest JSON in a last
// custom section, which takes the place of the one it was built with.
func manifested(t *testing.T, name, manifest string) string {
	t.Helper()
	wasm, err := os.ReadFile(filepath.Join("testdata", name, name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	body := append(binary.AppendUvarint(nil, 4), "fude"+manifest...)
	wasm = append(binary.AppendUvarint(append(wasm, 0), uint64(len(body))), body...)
	path := filepath.Join(t.TempDir(), name+".wasm")
	if err := os.WriteFile(path, wasm, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, path, fude string, g plugin.Grants, log func(string)) (*plugin.Plugin, error) {
	t.Helper()
	p, err := plugin.Load(context.Background(), path, g, fude, t.TempDir(), log, func(string, time.Duration) {})
	if err == nil {
		t.Cleanup(func() { p.Close(context.Background()) })
	}
	return p, err
}

// B603
func TestB603SDKVersions(t *testing.T) {
	for _, c := range []struct{ sdk, fude, want string }{
		{"v0.0.1", "", "built with sdk v0.0.1, and fude takes sdk " + plugin.OldestSDK + " or newer"},
		{"v99.0.0", plugin.OldestSDK, "needs fude v99.0.0 or newer"},
		{"v99.0.0", "2026.10.31 (1f4c5b5)", ""},
		{plugin.OldestSDK, plugin.OldestSDK, ""},
	} {
		_, err := load(t, manifested(t, "kit", `{"name":"kit","sdk":"`+c.sdk+`"}`), c.fude, plugin.Grants{}, func(string) {})
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Errorf("sdk %s on fude %q: %v, want %q", c.sdk, c.fude, err, c.want)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty.wasm")
	if err := os.WriteFile(empty, []byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(t, empty, "", plugin.Grants{}, func(string) {}); err == nil || !strings.Contains(err.Error(), "build it with fude plugin build") {
		t.Errorf("a plugin with no manifest: %v", err)
	}
}

// B604
func TestB604ManifestLimitsGrants(t *testing.T) {
	var logged []string
	p, err := load(t, manifested(t, "kit", `{"name":"kit","capabilities":["buffer.read"]}`), "", plugin.Grants{Capabilities: []string{"buffer.read", "buffer.write"}}, func(line string) { logged = append(logged, line) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Init(context.Background(), api.Scope{}); err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(p.Registered(), func(r api.Registration) bool { return r.Name == "Shout" })
	res, err := p.Call(context.Background(), p.Registered()[i].ID, api.Scope{}, 0, 0)
	if err != nil || len(res.Edits) > 0 || !slices.ContainsFunc(logged, func(l string) bool { return strings.Contains(l, "buffer.write") }) {
		t.Errorf(":Shout without buffer.write in the manifest asked for %v and logged %q, %v", res.Edits, logged, err)
	}
}
