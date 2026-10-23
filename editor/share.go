package editor

import (
	"bufio"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/relay"
	tlsshare "github.com/y3owk1n/fude/internal/share"
	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/wire"
)

// share is fude listening for guests (B248), or holding places for them
// at a relay (B251).
type share struct {
	addr    string // where guests reach fude, or the relay and room
	print   string // the certificate's SHA-256 fingerprint, base64url
	secret  string // the next guest's secret, "" once used
	waiting bool   // a join waits for the host's answer

	cfg     *tls.Config
	stopped chan struct{} // closed when the share ends
	via     string        // the relay's address, "" without one
	room    string
	lost    bool // the relay went away, until :share holds a new place

	mu    sync.Mutex // over place, which the share's end closes
	place *relay.Place
}

// guestHello is a guest's wire.Hello with what it takes from its config.toml.
// Its Own hides wire.Hello's, so the JSON decodes into the editor's type.
type guestHello struct {
	wire.Hello
	Own Own `json:"own"`
}

// Share listens on address with TLS for guests to join, and offers the
// share string as invite does (B248). It returns a func that stops
// listening and ends each join still waiting.
func (e *Session) Share(address string) (func(), error) {
	sh, err := newShare()
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	l, err := tls.Listen("tcp", address, sh.cfg)
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	if sh.addr, err = tlsshare.Reachable(l.Addr().(*net.TCPAddr), address); err != nil {
		l.Close()
		return nil, fmt.Errorf("share: %w", err)
	}
	e.share = sh
	e.invite()
	trace.Record("share", "listening at "+sh.addr, 0)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go e.admit(c, sh.stopped)
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(sh.stopped); l.Close() }) }, nil
}

// ShareRelay shares through the relay at addr, holding a place in a new
// room for guests to take, and offers the share string as invite does
// (B251). It returns a func that ends the share.
func (e *Session) ShareRelay(addr string) (func(), error) {
	sh, err := newShare()
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	sh.via, sh.room = addr, tlsshare.Secret()
	sh.addr = relay.WithPort(addr) + "/" + sh.room
	p, err := relay.Offer(addr, sh.room)
	if err != nil {
		return nil, fmt.Errorf("share through %s: %w", addr, err)
	}
	e.share = sh
	e.invite()
	trace.Record("share", "holding a place at "+addr, 0)
	e.hold(p)
	var once sync.Once
	return func() {
		once.Do(func() {
			close(sh.stopped)
			sh.mu.Lock()
			defer sh.mu.Unlock()
			if sh.place != nil {
				sh.place.Close()
			}
		})
	}, nil
}

// hold waits off the loop for a guest to take p, and holds a new place
// each time one does. When the relay goes away it says so (B251).
func (e *Session) hold(p *relay.Place) {
	sh := e.share
	sh.mu.Lock()
	sh.place = p
	sh.mu.Unlock()
	go func() {
		for {
			c, err := p.Take()
			if err == nil {
				trace.Record("share", "a guest came through "+sh.via, 0)
				go e.admit(tls.Server(c, sh.cfg), sh.stopped)
				p, err = relay.Offer(sh.via, sh.room)
			}
			sh.mu.Lock()
			select {
			case <-sh.stopped:
				if err == nil {
					p.Close()
				}
				sh.mu.Unlock()
				return
			default:
			}
			if err != nil {
				sh.place = nil
				sh.mu.Unlock()
				e.queue <- func() {
					sh.lost = true
					e.sessions[0].message = fmt.Sprintf("lost the relay at %s: %v, so guests cannot join until :share", sh.via, err)
				}
				return
			}
			sh.place = p
			sh.mu.Unlock()
		}
	}()
}

// newShare makes a share's certificate and the TLS that serves it.
func newShare() (*share, error) {
	cfg, print, err := tlsshare.Server()
	if err != nil {
		return nil, err
	}
	return &share{print: print, cfg: cfg, stopped: make(chan struct{})}, nil
}

// reshare shows a new share string, as :share does (B249). After the relay
// went away it holds a new place there first (B251).
func (e *Session) reshare() error {
	sh := e.share
	switch {
	case sh == nil:
		return errors.New("fude is not sharing, so start it with fude share")
	case sh.lost:
		// A second :share while this one waits finds the place being held.
		sh.lost = false
		e.message = "holding a new place at the relay"
		go func() {
			p, err := relay.Offer(sh.via, sh.room)
			e.queue <- func() {
				if err != nil {
					sh.lost = true
					e.message = fmt.Sprintf("the relay at %s: %v", sh.via, err)
					return
				}
				e.hold(p)
				e.invite()
			}
		}()
	default:
		e.invite()
	}
	return nil
}

// invite shows a new share string, and puts the command that joins with it
// on the clipboard, since the host sends it to the guest (B248).
func (e *Session) invite() {
	join := "fude join " + e.shareString()
	if err := (host{e}).SetClipboard(join); err != nil {
		e.message = fmt.Sprintf("copying failed with %v, so guests join with: %s", err, join)
		return
	}
	e.message = "copied, guests join with: " + join
}

// shareString makes a new secret and returns the string that lets one
// guest in with it (B249).
func (e *Session) shareString() string {
	e.share.secret = tlsshare.Secret()
	return tlsshare.Invite{Addr: e.share.addr, Print: e.share.print, Secret: e.share.secret}.String()
}

// admit lets the guest on c in once its secret is right and the host says
// yes, and then runs its session as an attach does (B248).
func (e *Session) admit(c net.Conn, stopped <-chan struct{}) {
	defer c.Close()
	on := func(f func()) {
		done := make(chan struct{})
		e.queue <- func() { f(); close(done) }
		<-done
	}
	in := bufio.NewScanner(c)
	in.Buffer(nil, 1<<26)
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hello guestHello
	if !in.Scan() || json.Unmarshal(in.Bytes(), &hello) != nil {
		trace.Record("share", c.RemoteAddr().String()+" sent no hello", 0)
		return
	}
	c.SetReadDeadline(time.Time{})
	var mu sync.Mutex
	enc := json.NewEncoder(c)
	out := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(v)
	}
	name := hello.Name
	if name == "" {
		name = c.RemoteAddr().String()
	}
	admitted := make(chan *Session, 1)
	var s *Session
	var err error
	on(func() { err = e.letIn(name, hello, admitted) })
	if err == nil {
		select {
		case s = <-admitted:
		case <-stopped:
			return
		}
		if s == nil {
			err = errors.New("the host said no")
		}
	}
	if err != nil {
		trace.Record("share", name+" was turned away: "+err.Error(), 0)
		out(map[string]string{"error": err.Error()})
		return
	}
	defer on(func() {
		trace.Record("share", s.name+" left", 0)
		s.leave()
	})
	if out(map[string]string{}) != nil {
		return
	}
	e.attached(s, in, out, on)
}

// letIn asks the host whether to let the guest called name in, when the
// secret it says hello with is the share's. It sends the guest's new
// session, which takes what the hello asks, on admitted, or nil when the
// host says no, and returns why it did not ask.
func (e *Session) letIn(name string, hello guestHello, admitted chan<- *Session) error {
	secret := hello.Secret
	host, sh := e.sessions[0], e.share
	switch {
	case sh.secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(sh.secret)) != 1:
		return errors.New("the share string is wrong or was used, so ask the host for a new one")
	case sh.waiting:
		return errors.New("another guest is waiting for the host, so try again soon")
	case host.ask != nil:
		return errors.New("the host is answering another question, so try again soon")
	}
	sh.waiting = true
	power, as := Guest, ""
	if hello.ReadOnly {
		power, as = ReadOnly, " read-only"
	}
	host.confirm(name+" wants to join"+as+", let them in?", func(yes bool) {
		sh.waiting = false
		if !yes {
			admitted <- nil
			return
		}
		// :share may have made a new secret since.
		if sh.secret == secret {
			sh.secret = ""
		}
		s := e.Attach(kernel.Origin("guest "+name), power)
		s.name = e.guestName(name)
		s.use(hello.Own)
		host.message = s.name + " joined"
		trace.Record("share", s.name+" joined"+as, 0)
		admitted <- s
	})
	return nil
}

// guestName returns name, or name with a number after it when a session
// has it (B250).
func (e *Session) guestName(name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(e.sessions, func(s *Session) bool { return s.name == n })
	}
	unique := name
	for i := 2; taken(unique); i++ {
		unique = name + strconv.Itoa(i)
	}
	return unique
}

// kick ends the session of the guest called name, as :kick does. A guest
// may not (B244, B250).
func (e *Session) kick(name string) {
	if e.power != Host {
		e.message = "a guest may not run :kick"
		return
	}
	i := slices.IndexFunc(e.sessions, func(s *Session) bool { return s.power != Host && s.label() == name })
	if i < 0 {
		e.message = "no guest is called " + name
		return
	}
	s := e.sessions[i]
	s.message = "the host ended your session"
	s.leave()
	e.message = "ended " + name + "'s session"
}

// sharing says, for the status line, whether fude shares and with how many
// guests, or that the session is a guest (B269).
func (e *Session) sharing() string {
	switch {
	case e.power == Guest:
		return "guest"
	case e.power == ReadOnly:
		return "read-only guest"
	case e.share == nil:
		return ""
	}
	guests := 0
	for _, s := range e.sessions {
		if s.power != Host {
			guests++
		}
	}
	if guests == 0 {
		return "sharing"
	}
	return fmt.Sprintf("sharing with %d", guests)
}
