// Package web shows a session in a browser on this machine, a frontend of
// client.Session as tui and gui are. The session runs here and sends the
// page each window's changed cells and the rest of the frame as data, and
// the page sends back the keys typed there (D79).
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/internal/shell"
	"github.com/y3owk1n/fude/vim"
)

//go:embed page.html
var page []byte

// Run shows s at a local address with a secret in its URL until the
// session ends. It opens the address in the system's browser and prints it,
// to open again (B606).
func Run(s client.Session) error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	token := rand.Text()
	url := fmt.Sprintf("http://%s/?t=%s", l.Addr(), token)
	fmt.Println("fude shows at", url)
	go func() {
		argv := shell.Opener(url)
		if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
			fmt.Printf("open a browser: %v: %s\n", err, bytes.TrimSpace(out))
		}
	}()
	return serve(s, l, token)
}

// viewer is the page that shows the session, which gets each frame.
type viewer struct {
	frames chan []byte
	gone   <-chan struct{}
}

// serve runs s and serves it on l to a page that knows token. A page that
// opens later takes the session from the one before it.
func serve(s client.Session, l net.Listener, token string) error {
	keys := make(chan vim.Key, 256)
	pastes := make(chan string)
	sizes := make(chan [4]int)
	var pngs sync.Map // the PNGs of the images the page shows, by id (B303)
	joins := make(chan viewer)
	host := l.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'")
		w.Write(page)
	})
	mux.HandleFunc("GET /frames", func(w http.ResponseWriter, r *http.Request) {
		v := viewer{frames: make(chan []byte), gone: r.Context().Done()}
		select {
		case joins <- v:
		case <-v.gone:
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		for {
			select {
			case b, ok := <-v.frames:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", b)
				if http.NewResponseController(w).Flush() != nil {
					return // the page went
				}
			case <-v.gone:
				return
			}
		}
	})
	mux.HandleFunc("GET /images/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		png, ok := pngs.Load(id)
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(png.([]byte))
	})
	mux.HandleFunc("POST /keys", func(w http.ResponseWriter, r *http.Request) {
		var ks []string
		if err := json.NewDecoder(r.Body).Decode(&ks); err != nil {
			http.Error(w, "keys: "+err.Error(), http.StatusBadRequest)
			return
		}
		for _, k := range ks {
			select {
			case keys <- vim.Key(k):
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /paste", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "paste: "+err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case pastes <- string(b):
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("POST /size", func(w http.ResponseWriter, r *http.Request) {
		var size [4]int
		if err := json.NewDecoder(r.Body).Decode(&size); err != nil || size[0] < 1 || size[1] < 1 {
			http.Error(w, "size: want [cols, rows, cell width, cell height]", http.StatusBadRequest)
			return
		}
		select {
		case sizes <- size:
		case <-r.Context().Done():
		}
	})
	srv := &http.Server{Handler: guard(mux, host, token)}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(l) }()

	p := painter{pngs: &pngs}
	var cur *viewer
	var last []byte // the frame sent last, to skip one that changes nothing
	draw := func(ended bool) {
		if cur == nil {
			return
		}
		f := p.paint(s)
		f.Ended = ended
		b, err := json.Marshal(f)
		if err != nil {
			panic(fmt.Sprintf("web: frame: %v", err)) // a frame is plain data
		}
		if bytes.Equal(b, last) {
			return
		}
		last = b
		if !send(cur, b) {
			cur = nil
		}
	}
	s.SetSize(80, 24)
	s.Load()
	var timeout <-chan time.Time
	for !s.Done() {
		select {
		case k := <-keys:
			s.Feed(k)
		case f := <-s.Queue():
			f()
		case text := <-pastes:
			s.Paste(text)
		case size := <-sizes:
			s.SetSize(size[0], size[1])
			s.SetCell(size[2], size[3])
		case v := <-joins:
			if cur != nil {
				send(cur, []byte(`{"moved":true}`))
				close(cur.frames)
			}
			cur, p.grids = &v, nil
		case <-timeout:
			timeout = nil
			s.Timeout()
		case err := <-served:
			return fmt.Errorf("web: %w", err)
		}
		if d, ok := s.Wait(); !ok {
			timeout = nil
		} else if timeout == nil {
			timeout = time.After(d)
		}
		if len(keys) > 0 {
			continue
		}
		draw(false)
	}
	draw(true)
	if cur != nil {
		close(cur.frames)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("web: %w", err)
	}
	return nil
}

// send gives v a frame, and reports whether v is still there.
func send(v *viewer, b []byte) bool {
	select {
	case v.frames <- b:
		return true
	case <-v.gone:
		return false
	}
}

// guard lets through only requests with the token, to the address served,
// and posts from the page itself, so no other site or name that points
// here reaches the session (B607).
func guard(next http.Handler, host, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given := r.URL.Query().Get("t")
		origin := r.Header.Get("Origin")
		switch {
		case r.Host != host,
			subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1,
			r.Method == http.MethodPost && origin != "http://"+host:
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
