package web

import (
	"bufio"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor/editortest"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// served serves a new editor on a local address and returns its address,
// and a channel closed once serve returns. The editor quits through the
// page when the test ends, if the test did not quit it, and serve must
// return without an error.
func served(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	e := editortest.Start(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + l.Addr().String()
	finished := make(chan struct{})
	var served error
	go func() {
		served = serve(e, l, "secret")
		close(finished)
	}()
	t.Cleanup(func() {
		select {
		case <-finished:
		default:
			post(t, base, "/keys", `["<Esc>",":","q","a","!","<CR>"]`)
			<-finished
		}
		if served != nil {
			t.Errorf("serve: %v", served)
		}
	})
	return base, finished
}

func post(t *testing.T, base, path, body string) int {
	t.Helper()
	req, err := http.NewRequest("POST", base+path+"?t=secret", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// B606
func TestB606KeysTypedInThePageReachTheSession(t *testing.T) {
	base, finished := served(t)
	resp, err := http.Get(base + "/frames?t=secret")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := make(chan string)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				frames <- data
			}
		}
		close(frames)
	}()
	until := func(want string) {
		t.Helper()
		deadline := time.After(editortest.Patience)
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					t.Fatalf("the frames ended before one held %s", want)
				}
				if strings.Contains(f, want) {
					return
				}
			case <-deadline:
				t.Fatalf("no frame held %s after %s", want, editortest.Patience)
			}
		}
	}
	if code := post(t, base, "/size", "[60,10]"); code != http.StatusOK {
		t.Fatalf("size: %d", code)
	}
	if code := post(t, base, "/keys", `["i","w","e","b","b","y","<Esc>"]`); code != http.StatusOK {
		t.Fatalf("keys: %d", code)
	}
	until(`"webby"`)
	post(t, base, "/keys", `[":","q","a","!","<CR>"]`)
	until(`"ended":true`)
	<-finished
}

// B607
func TestB607OnlyThePageReachesTheSession(t *testing.T) {
	base, _ := served(t)
	// get gets url by the host name given, or by its address for "".
	get := func(url, host string) int {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if host != "" {
			req.Host = host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get(base+"/?t=secret", ""); code != http.StatusOK {
		t.Errorf("the page with its secret gave %d", code)
	}
	if code := get(base+"/?t=guess", ""); code != http.StatusForbidden {
		t.Errorf("a wrong secret gave %d", code)
	}
	if code := get(base+"/?t=secret", "attacker.example"); code != http.StatusForbidden {
		t.Errorf("another host name gave %d", code)
	}
	req, err := http.NewRequest("POST", base+"/keys?t=secret", strings.NewReader(`["x"]`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://attacker.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a post from another site gave %d", resp.StatusCode)
	}
}
