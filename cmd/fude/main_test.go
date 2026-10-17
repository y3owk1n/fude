package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/plugin"
	"github.com/y3owk1n/fude/plugin/plugintest"
)

// TestMain runs fude itself when a test runs this binary as the CLI.
func TestMain(m *testing.M) {
	if os.Getenv("FUDE_TEST_CLI") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// B173
func TestB173PluginNewAndBuild(t *testing.T) {
	dir := t.TempDir()
	fude := func(args ...string) (string, error) {
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir, cmd.Env = dir, append(os.Environ(), "FUDE_TEST_CLI=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	// The new plugin takes the sdk through the module proxy.
	if out, err := fude("plugin", "new", "words"); err != nil {
		if strings.Contains(out, "dial tcp") || strings.Contains(out, "no such host") {
			t.Skipf("the module proxy is out of reach: %s", out)
		}
		t.Fatalf("fude plugin new words: %v: %s", err, out)
	}
	for _, name := range []string{"go.mod", "main.go"} {
		if _, err := os.Stat(filepath.Join(dir, "words", name)); err != nil {
			t.Fatalf("plugin new wrote no %s: %v", name, err)
		}
	}
	if _, err := exec.LookPath("tinygo"); err != nil {
		t.Skip("tinygo is not installed, so the plugin is not built")
	}
	if out, err := fude("plugin", "build", "words"); err != nil {
		t.Fatalf("fude plugin build words: %v: %s", err, out)
	}
	// The plugin it built runs :Words as any plugin's command runs.
	words := plugintest.Load(t, filepath.Join(dir, "words", "words.wasm"), plugin.Grants{Capabilities: []string{"buffer.read", "ui"}}, "")
	if res := words.Command("Words", "", "one two three", 0); res.Status == nil || *res.Status != "3 words" {
		t.Errorf(":Words showed %v, want 3 words", res.Status)
	}
}
