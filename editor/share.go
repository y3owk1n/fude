package editor

import (
	"bufio"
	"cmp"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

// share is fude listening for guests (B248).
type share struct {
	addr    string // where guests reach fude
	print   string // the certificate's SHA-256 fingerprint, base64url
	secret  string // the next guest's secret, "" once used
	waiting bool   // a join waits for the host's answer
}

// Hello is the first line a joining guest sends.
type Hello struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

// Share listens on address with TLS for guests to join, and offers the
// share string as invite does (B248). It returns a func that stops
// listening and ends each join still waiting.
func (e *Session) Share(address string) (func(), error) {
	cert, print, err := certificate()
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	l, err := tls.Listen("tcp", address, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
	if err != nil {
		return nil, fmt.Errorf("share: %w", err)
	}
	addr, err := reachable(l.Addr().(*net.TCPAddr), address)
	if err != nil {
		l.Close()
		return nil, fmt.Errorf("share: %w", err)
	}
	e.share = &share{addr: addr, print: print}
	e.invite()
	stopped := make(chan struct{})
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go e.admit(c, stopped)
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stopped); l.Close() }) }, nil
}

// reshare shows a new share string, as :share does (B249).
func (e *Session) reshare() {
	if e.share == nil {
		e.message = "fude is not sharing, so start it with fude --share"
		return
	}
	e.invite()
}

// invite shows a new share string, and puts the command that joins with it
// on the clipboard, since the host sends it to the guest (B248).
func (e *Session) invite() {
	join := "fude --join " + e.shareString()
	if err := (host{e}).SetClipboard(join); err != nil {
		e.message = fmt.Sprintf("copying failed with %v, so guests join with: %s", err, join)
		return
	}
	e.message = "copied, guests join with: " + join
}

// shareString makes a new secret and returns the string that lets one
// guest in with it (B249).
func (e *Session) shareString() string {
	secret := make([]byte, 16)
	rand.Read(secret)
	e.share.secret = base64.RawURLEncoding.EncodeToString(secret)
	return e.share.addr + "/" + e.share.print + "/" + e.share.secret
}

// certificate makes a self-signed certificate for a share, and returns it
// with its fingerprint.
func certificate() (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	template := &x509.Certificate{SerialNumber: serial, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// reachable returns where a guest reaches the listener at a, which listens
// on address: its host, or else this machine's first address that is not a
// loopback.
func reachable(a *net.TCPAddr, address string) (string, error) {
	port := fmt.Sprint(a.Port)
	if host, _, err := net.SplitHostPort(address); err == nil && host != "" && !net.ParseIP(host).IsUnspecified() {
		return net.JoinHostPort(host, port), nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	for _, ad := range addrs {
		if ip, ok := ad.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
			return net.JoinHostPort(ip.IP.String(), port), nil
		}
	}
	return "", errors.New("this machine has no address but loopback, so name one")
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
	var hello Hello
	if !in.Scan() || json.Unmarshal(in.Bytes(), &hello) != nil {
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
	on(func() { err = e.letIn(name, hello.Secret, admitted) })
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
		out(map[string]string{"error": err.Error()})
		return
	}
	defer on(func() { s.leave() })
	if out(map[string]string{}) != nil {
		return
	}
	e.attached(s, in, out, on)
}

// letIn asks the host whether to let the guest called name in, when its
// secret is the share's. It sends the guest's new session on admitted, or
// nil when the host says no, and returns why it did not ask.
func (e *Session) letIn(name, secret string, admitted chan<- *Session) error {
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
	host.message = name + " wants to join, let them in? (y/n)"
	host.ask = func(yes bool) {
		sh.waiting = false
		if !yes {
			host.message = ""
			admitted <- nil
			return
		}
		// :share may have made a new secret since.
		if sh.secret == secret {
			sh.secret = ""
		}
		s := e.Attach(kernel.Origin("guest "+name), Guest)
		s.name = e.guestName(name)
		host.message = s.name + " joined"
		admitted <- s
	}
	return nil
}

// guestName returns name, or name with a number after it when a guest
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

// who shows the guests' names, as :who does (B250).
func (e *Session) who() {
	var names []string
	for _, s := range e.sessions {
		if s.name != "" {
			names = append(names, s.name)
		}
	}
	e.message = cmp.Or(strings.Join(names, "  "), "no guests")
}

// kick ends the session of the guest called name, as :kick does (B250).
func (e *Session) kick(name string) {
	i := slices.IndexFunc(e.sessions, func(s *Session) bool { return s.name != "" && s.name == name })
	if i < 0 {
		e.message = "no guest is called " + name
		return
	}
	s := e.sessions[i]
	s.message = "the host ended your session"
	s.leave()
	e.message = "ended " + name + "'s session"
}
