package editortest

import (
	"encoding/binary"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// WithManifest returns wasm with the manifest JSON j in a last custom
// section, which takes the place of the module's own.
func WithManifest(wasm []byte, j string) []byte {
	body := append(binary.AppendUvarint(nil, 4), "fude"+j...)
	return append(binary.AppendUvarint(append(slices.Clip(wasm), 0), uint64(len(body))), body...)
}

// Repo serves a git repo named kit over http, with a commit of files and
// a commit after it for each of tags, and the .wasm of a release from
// assets by its tag. It returns the repo's URL. It skips the test without
// git.
func Repo(t *testing.T, assets *sync.Map, files map[string][]byte, tags ...string) string {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skipf("git is not installed: %v", err)
	}
	work, root := t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(work, "init", "-q")
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(work, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run(work, "add", ".")
	run(work, "commit", "-q", "--allow-empty", "-m", "kit")
	for _, tag := range tags {
		run(work, "commit", "-q", "--allow-empty", "-m", tag)
		run(work, "tag", tag)
	}
	run(root, "clone", "-q", "--bare", work, "kit.git")
	backend := &cgi.Handler{Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"), Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag, ok := strings.CutPrefix(r.URL.Path, "/kit/releases/download/")
		if !ok {
			backend.ServeHTTP(w, r)
			return
		}
		if b, ok := assets.Load(strings.TrimSuffix(tag, "/kit.wasm")); ok {
			w.Write(b.([]byte))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/kit.git"
}
