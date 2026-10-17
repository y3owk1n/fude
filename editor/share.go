package editor

import (
	"bufio"
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

// Share listens on address with TLS for guests to join, and shows the
// share string as e's message (B248). It returns a func that stops
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
	e.message = "guests join with: fude --join " + e.shareString()
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
	answered := make(chan error, 1)
	var err error
	on(func() { err = e.letIn(name, hello.Secret, answered) })
	if err == nil {
		select {
		case err = <-answered:
		case <-stopped:
			return
		}
	}
	if err != nil {
		out(map[string]string{"error": err.Error()})
		return
	}
	var s *Session
	on(func() { s = e.Attach(kernel.Origin("guest "+name), Guest) })
	defer on(func() { s.leave() })
	if out(map[string]string{}) != nil {
		return
	}
	e.attached(s, in, out, on)
}

// letIn asks the host whether to let the guest called name in, when its
// secret is the share's, and sends the answer on answered, nil for yes. It
// returns why it did not ask.
func (e *Session) letIn(name, secret string, answered chan<- error) error {
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
			answered <- errors.New("the host said no")
			return
		}
		sh.secret = ""
		host.message = name + " joined"
		answered <- nil
	}
	return nil
}
