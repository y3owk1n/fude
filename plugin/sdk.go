package plugin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
)

// fudeModule is the Go module of the fude program.
const fudeModule = "github.com/y3owk1n/fude/cmd/fude"

// sdkModule is the Go module plugins are written with.
const sdkModule = "github.com/y3owk1n/fude/sdk"

// template is the main.go fude plugin new writes.
const template = `// Package main is a fude plugin. "fude plugin build" builds it into a
// .wasm, which a [[plugin]] table in config.toml loads. With path set to
// this folder, fude builds it when its source changes:
//
//	[[plugin]]
//	path = "/path/to/NAME"
//	capabilities = ["buffer.read", "ui"]
package main

import (
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Command("Words", "shows how many words the buffer has", func(sdk.Call) {
		n, err := sdk.Len()
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		text, err := sdk.Text(0, n)
		if err != nil {
			sdk.Log(err.Error())
			return
		}
		sdk.Status(strconv.Itoa(len(strings.Fields(text))) + " words")
	})
}

func main() {}
`

// manifestTemplate is the fude.toml fude plugin new writes.
const manifestTemplate = `# The manifest. fude plugin build writes it into the plugin, and
# fude plugin add grants what it asks for.
name = "NAME"
capabilities = ["buffer.read", "ui"]
programs = []
hosts = []
`

// workflow is the release workflow fude plugin new writes. On a tag v* it
// builds the plugin and attaches its .wasm to the release, where fude
// installs it from (D76).
const workflow = `name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
      - uses: acifani/setup-tinygo@v3
        with:
          tinygo-version: "0.43.0"
      - name: Build and attach the plugin
        env:
          GH_TOKEN: ${{ github.token }}
          WASM: ${{ github.event.repository.name }}.wasm
        run: |
          curl -fsSL -o fude.zip https://github.com/y3owk1n/fude/releases/latest/download/fude-linux-amd64.zip
          unzip -q fude.zip -d "$RUNNER_TEMP/fude"
          "$RUNNER_TEMP/fude/bin/fude" plugin build
          gh release view "$GITHUB_REF_NAME" >/dev/null 2>&1 || gh release create "$GITHUB_REF_NAME" --verify-tag --generate-notes
          gh release upload "$GITHUB_REF_NAME" "$WASM" --clobber
`

// release is a version fude and the sdk are both tagged with, as
// v1.2.3 and sdk/v1.2.3.
var release = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// New writes a plugin in a new folder at dir: a go.mod that requires the
// sdk of this fude's release, or the newest, a main.go that adds the
// command :Words, its fude.toml and a release workflow (B602). It removes
// the folder when it cannot get the sdk.
func New(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("plugin new: %s already exists", dir)
	}
	name := filepath.Base(dir)
	if !plainName.MatchString(name) {
		return fmt.Errorf("plugin new: name %q is not lowercase letters, digits, - and _", name)
	}
	files := map[string]string{
		"go.mod":                        "module " + name + "\n\ngo 1.26\n",
		"main.go":                       strings.ReplaceAll(template, "NAME", name),
		"fude.toml":                     strings.ReplaceAll(manifestTemplate, "NAME", name),
		".github/workflows/release.yml": workflow,
	}
	for path, text := range files {
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("plugin new: %w", err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return fmt.Errorf("plugin new: %w", err)
		}
	}
	// A build of fude that is not a release, or another program, takes the
	// newest sdk.
	version := "latest"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path == fudeModule && release.MatchString(info.Main.Version) {
		version = info.Main.Version
	}
	// go get marks the sdk indirect, and tidy marks it direct once it reads
	// main.go's imports.
	for _, args := range [][]string{{"get", sdkModule + "@" + version}, {"mod", "tidy"}} {
		if out, err := goCommand(dir, args...).CombinedOutput(); err != nil {
			return errors.Join(fmt.Errorf("plugin new: go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out))), os.RemoveAll(dir))
		}
	}
	return nil
}

// Build builds the plugin in dir with TinyGo, into a .wasm named after the
// folder that carries its fude.toml and the sdk it built with, and returns
// its path. It builds for the target the sdk ships, which gives the plugin
// a collector that frees memory (B602).
func Build(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("plugin build: %w", err)
	}
	m, err := readManifest(dir)
	if err != nil {
		return "", fmt.Errorf("plugin build: %w", err)
	}
	tinygo, err := exec.LookPath("tinygo")
	if err != nil {
		return "", errors.New("plugin build: tinygo is not installed, see https://tinygo.org/getting-started/install")
	}
	// A replaced sdk has no version, nor does one at a commit.
	out, err := goCommand(dir, "list", "-m", "-f", "{{.Dir}}\n{{if not .Replace}}{{.Version}}{{end}}", sdkModule).Output()
	if err != nil {
		return "", fmt.Errorf("plugin build: find %s in %s: %w", sdkModule, dir, err)
	}
	sdkDir, version, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if release.MatchString(version) {
		m.SDK = version
	}
	target := filepath.Join(sdkDir, "fude.json")
	wasm := filepath.Join(dir, filepath.Base(dir)+".wasm")
	// Debug info would make a plugin twenty times its size.
	cmd := exec.Command(tinygo, "build", "-no-debug", "-target="+target, "-o", wasm, ".")
	cmd.Dir, cmd.Env = dir, append(os.Environ(), "GOWORK=off")
	// TinyGo finds its library beside its binary, and a link to the binary
	// hides where that is.
	if os.Getenv("TINYGOROOT") == "" {
		if real, err := filepath.EvalSymlinks(tinygo); err == nil && real != tinygo {
			cmd.Env = append(cmd.Env, "TINYGOROOT="+filepath.Dir(filepath.Dir(real)))
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("plugin build: %w: %s", err, strings.TrimSpace(string(out)))
	}
	b, err := os.ReadFile(wasm)
	if err == nil {
		b, err = withManifest(b, m)
	}
	if err == nil {
		err = os.WriteFile(wasm, b, 0o644)
	}
	if err != nil {
		return "", fmt.Errorf("plugin build: %w", err)
	}
	return wasm, nil
}

// goCommand runs go with args in dir, outside any workspace, as a plugin is
// a module of its own.
func goCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), "GOWORK=off")
	return cmd
}
