// Package install installs a repo's plugin and keeps plugins.lock, which
// pins the commit of each (D76).
package install

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/plugin"
)

// Entry is a repo's plugin as plugins.lock pins it. SHA256 is set only for
// a .wasm from a release, as a build differs by machine (B597).
type Entry struct {
	Repo    string `toml:"repo"`
	Version string `toml:"version"`
	Commit  string `toml:"commit"`
	SHA256  string `toml:"sha256,omitempty"`
}

// Lock is plugins.lock.
type Lock struct {
	Plugin []Entry `toml:"plugin"`
}

// lockPath is where plugins.lock is, beside config.toml.
func lockPath() (string, error) {
	dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
	return filepath.Join(dir, "fude", "plugins.lock"), err
}

// storeDir is where installed plugins are.
func storeDir() (string, error) {
	dir, err := keep.Dir("XDG_DATA_HOME", filepath.Join(".local", "share"))
	return filepath.Join(dir, "fude", "plugins"), err
}

// Read reads plugins.lock, empty when there is none.
func Read() (Lock, error) {
	var l Lock
	p, err := lockPath()
	if err != nil {
		return l, fmt.Errorf("read plugins.lock: %w", err)
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	if err == nil {
		err = toml.Unmarshal(b, &l)
	}
	if err != nil {
		return l, fmt.Errorf("read %s: %w", p, err)
	}
	return l, nil
}

// Find returns repo's entry.
func (l Lock) Find(repo string) (Entry, bool) {
	i := slices.IndexFunc(l.Plugin, func(e Entry) bool { return e.Repo == repo })
	if i < 0 {
		return Entry{}, false
	}
	return l.Plugin[i], true
}

// Put sets e's entry and drops those of repos not in repos, the repos
// config.toml lists.
func (l *Lock) Put(e Entry, repos []string) {
	l.Plugin = slices.DeleteFunc(l.Plugin, func(o Entry) bool { return o.Repo == e.Repo || !slices.Contains(repos, o.Repo) })
	l.Plugin = append(l.Plugin, e)
	slices.SortFunc(l.Plugin, func(a, b Entry) int { return cmp.Compare(a.Repo, b.Repo) })
}

// Write writes plugins.lock whole.
func (l Lock) Write() error {
	p, err := lockPath()
	if err == nil {
		var b []byte
		if b, err = toml.Marshal(l); err == nil {
			err = keep.Write(p, b)
		}
	}
	if err != nil {
		return fmt.Errorf("write plugins.lock: %w", err)
	}
	return nil
}

// File is where e's .wasm is installed.
func File(e Entry) (string, error) {
	dir, err := storeDir()
	return filepath.Join(dir, repoDir(e.Repo), e.Commit+".wasm"), err
}

// Prune removes the installed files of repos not in repos.
func Prune(repos []string) error {
	dir, err := storeDir()
	if err != nil {
		return fmt.Errorf("prune plugins: %w", err)
	}
	kept := map[string]bool{}
	for _, r := range repos {
		kept[filepath.Join(dir, repoDir(r))] = true
	}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return filepath.SkipAll
		case err != nil:
			return err
		case !d.IsDir() && !kept[filepath.Dir(p)]:
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("prune plugins: %w", err)
	}
	return nil
}

// repoDir is the folder under the store that a repo's files are in.
func repoDir(repo string) string {
	if _, rest, ok := strings.Cut(repo, "://"); ok {
		repo = rest
	}
	var parts []string
	for _, p := range strings.FieldsFunc(strings.TrimSuffix(repo, ".git"), func(r rune) bool { return r == '/' || r == '\\' || r == ':' }) {
		if p != "." && p != ".." {
			parts = append(parts, p)
		}
	}
	return filepath.Join(parts...)
}

// Name is the last part of repo, which its release's .wasm is named after.
func Name(repo string) string { return path.Base(strings.TrimSuffix(repo, ".git")) }

// remote returns the URL git fetches repo from, and the web address its
// releases are under, "" for a repo not on http.
func remote(repo string) (url, web string) {
	url = repo
	if !strings.Contains(repo, "://") && !strings.Contains(repo, "@") && !filepath.IsAbs(repo) && strings.Count(repo, "/") == 2 {
		url = "https://" + repo
	}
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		web = strings.TrimSuffix(url, ".git")
	}
	return url, web
}

var (
	release = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	hash    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Pinned reports whether version names a release or a commit, which
// :plugin update leaves as it is (B599).
func Pinned(version string) bool { return release.MatchString(version) || hash.MatchString(version) }

// candidate is a commit of the repo that version may resolve to.
type candidate struct {
	version, commit string
	release         bool // a tag vX.Y.Z, which may carry a .wasm
}

// Fetch returns the .wasm of repo's plugin at version, which is a tag,
// branch or commit, with its entry and manifest. For "" it takes the
// newest release that fude at version host loads. It takes the .wasm a
// release carries, and builds the commit otherwise. It reuses locked's
// file when the commit is the one locked, and refuses a release's .wasm
// whose sha256 is not locked's. It runs git and http, so it belongs off
// the editor loop (B597, B598, B603).
func Fetch(ctx context.Context, repo, version string, locked Entry, host string) (Entry, plugin.Manifest, []byte, error) {
	url, web := remote(repo)
	cands, err := resolve(ctx, url, version)
	if err != nil {
		return Entry{}, plugin.Manifest{}, nil, fmt.Errorf("install %s: %w", repo, err)
	}
	var first error
	for _, c := range cands {
		wasm, sum, err := fetch(ctx, url, web, Name(repo), c, locked)
		if err != nil {
			return Entry{}, plugin.Manifest{}, nil, fmt.Errorf("install %s: %w", repo, err)
		}
		m, err := plugin.ReadManifest(wasm)
		switch {
		case err != nil:
			return Entry{}, m, nil, fmt.Errorf("install %s: %w", repo, err)
		case m.SDK == "":
			return Entry{}, m, nil, fmt.Errorf("install %s: it was built with an sdk that is no release, so fude loads it only from a path", repo)
		}
		// An older release may suit this fude.
		if err := m.Check(host); err != nil {
			first = cmp.Or(first, err)
			continue
		}
		return Entry{Repo: repo, Version: c.version, Commit: c.commit, SHA256: sum}, m, wasm, nil
	}
	return Entry{}, plugin.Manifest{}, nil, first
}

// resolve returns what version may be in the repo at url, newest first.
func resolve(ctx context.Context, url, version string) ([]candidate, error) {
	tags, err := refs(ctx, url, []string{"--tags"})
	if err != nil {
		return nil, err
	}
	switch {
	case version == "":
		var out []candidate
		for ref, c := range tags {
			if tag := strings.TrimPrefix(ref, "refs/tags/"); release.MatchString(tag) {
				out = append(out, candidate{tag, c, true})
			}
		}
		slices.SortFunc(out, func(a, b candidate) int { return slices.Compare(parts(b.version), parts(a.version)) })
		if len(out) > 0 {
			return out, nil
		}
		head, err := refs(ctx, url, nil, "HEAD")
		if err != nil || head["HEAD"] == "" {
			return nil, cmp.Or(err, errors.New("it has no release and no HEAD"))
		}
		return []candidate{{"", head["HEAD"], false}}, nil
	case tags["refs/tags/"+version] != "":
		return []candidate{{version, tags["refs/tags/"+version], release.MatchString(version)}}, nil
	}
	heads, err := refs(ctx, url, nil, "refs/heads/"+version)
	switch c := heads["refs/heads/"+version]; {
	case err != nil:
		return nil, err
	case c != "":
		return []candidate{{version, c, false}}, nil
	case hash.MatchString(version):
		// A release's commit takes the release's .wasm.
		for ref, c := range tags {
			if tag := strings.TrimPrefix(ref, "refs/tags/"); c == version && release.MatchString(tag) {
				return []candidate{{tag, c, true}}, nil
			}
		}
		return []candidate{{version, version, false}}, nil
	}
	return nil, fmt.Errorf("it has no tag, branch or commit %s", version)
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

// refs returns the commit of each ref git ls-remote lists at url, with
// flags before the URL and patterns after it.
func refs(ctx context.Context, url string, flags []string, patterns ...string) (map[string]string, error) {
	args := append(append(append([]string{"ls-remote"}, flags...), url), patterns...)
	out, err := git(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for line := range strings.Lines(out) {
		c, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		// An annotated tag's commit follows it, as tag^{}.
		m[strings.TrimSuffix(ref, "^{}")] = c
	}
	return m, nil
}

// git runs git with args in dir, never asking for a password.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is not installed")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// fetch returns the .wasm of c and its sha256 when it came from a release.
func fetch(ctx context.Context, url, web, name string, c candidate, locked Entry) ([]byte, string, error) {
	if c.commit == locked.Commit {
		if p, err := File(locked); err == nil {
			if b, err := os.ReadFile(p); err == nil {
				return b, locked.SHA256, nil
			}
		}
	}
	if c.release && web != "" {
		b, err := download(ctx, web+"/releases/download/"+c.version+"/"+name+".wasm")
		if err != nil {
			return nil, "", err
		}
		if b != nil {
			h := sha256.Sum256(b)
			sum := hex.EncodeToString(h[:])
			if c.commit == locked.Commit && locked.SHA256 != "" && sum != locked.SHA256 {
				return nil, "", fmt.Errorf("the .wasm of %s has sha256 %s, not the %s plugins.lock pins", c.version, sum, locked.SHA256)
			}
			return b, sum, nil
		}
	}
	b, err := build(ctx, url, c.commit, name)
	return b, "", err
}

// download gets url, or nil for a 404.
func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("get %s: %s", url, resp.Status)
	}
	// A plugin is at most its 64MB of memory.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	return b, nil
}

// build fetches commit alone from url into a folder named name and builds
// the plugin there with TinyGo.
func build(ctx context.Context, url, commit, name string) ([]byte, error) {
	work, err := os.MkdirTemp("", "fude-plugin-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	dir := filepath.Join(work, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, err
	}
	for _, step := range [][]string{{"init", "-q"}, {"fetch", "-q", "--depth", "1", url, commit}, {"checkout", "-q", "FETCH_HEAD"}} {
		if _, err := git(ctx, dir, step...); err != nil {
			return nil, err
		}
	}
	wasm, err := plugin.Build(dir)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(wasm)
}

// Store installs e's .wasm and removes the files of the repo's other
// commits, as nothing loads them now.
func Store(e Entry, wasm []byte) error {
	p, err := File(e)
	if err == nil {
		err = keep.Write(p, wasm)
	}
	if err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(filepath.Dir(p), "*.wasm"))
	for _, o := range old {
		if o != p {
			err = errors.Join(err, os.Remove(o))
		}
	}
	return err
}
