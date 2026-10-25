package plugintest_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/plugin/plugintest"
)

// wasm returns the path of the test plugin name.
func wasm(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "testdata", name, name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// B192
func TestB192TestHost(t *testing.T) {
	kit := plugintest.Load(t, wasm(t, "kit"), plugin.Grants{Capabilities: []string{"buffer.read", "buffer.write"}}, "")
	if got := plugintest.Apply("ab", kit.Command("Shout", "", "ab", 1)); got != "a!b" {
		t.Errorf(":Shout made %q, want a!b", got)
	}
	if got := plugintest.Apply("ab", kit.Key("gS", "ab", 0, 1)); got != "!ab" {
		t.Errorf("gS made %q, want !ab", got)
	}
	// A call reads the plugin's options as its table starts them.
	ctx := plugintest.Load(t, wasm(t, "ctx"), plugin.Grants{Capabilities: []string{"buffer.write"}, Options: map[string]string{"depth": "3"}}, "")
	if got := plugintest.Apply("", ctx.Command("Opt", "ctx.depth", "", 0)) + plugintest.Apply("", ctx.Command("Opt", "ctx.on", "", 0)); got != "3|false|" {
		t.Errorf(":Opt typed %q, want 3|false|", got)
	}
	// A call is about the file and filetype the test gives.
	ctx.Path, ctx.Filetype = "notes.md", "markdown"
	if got := plugintest.Apply("", ctx.Command("Where", "", "", 0)); !strings.Contains(got, "notes.md") || !strings.Contains(got, "markdown") {
		t.Errorf(":Where typed %q, want the path and filetype", got)
	}
	listen := plugintest.Load(t, wasm(t, "listen"), plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	res := listen.Event("buffer.changed", "abc")
	if len(res.Decorate) != 1 || res.Decorate[0].Style != "keyword" || !slices.Contains(listen.Log(), "listen: buffer.changed") {
		t.Errorf("buffer.changed asked for %+v and logged %q", res.Decorate, listen.Log())
	}
}
