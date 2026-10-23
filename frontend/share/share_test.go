package share_test

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/y3owk1n/fude/frontend/share"
)

// B248, B249
func TestGuestTrustsTheFingerprintedCertificateAlone(t *testing.T) {
	server, print, err := share.Server()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := share.Parse(share.Invite{Addr: "relay:7418/room", Print: print, Secret: share.Secret()}.String())
	if err != nil || inv.Addr != "relay:7418/room" || inv.Print != print {
		t.Fatalf("the share string parsed to %+v, %v", inv, err)
	}
	l, err := tls.Listen("tcp", "127.0.0.1:0", server)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	addr := l.Addr().(*net.TCPAddr).String()
	if c, err := tls.Dial("tcp", addr, share.Client(print)); err != nil {
		t.Errorf("the guest did not trust the certificate: %v", err)
	} else {
		c.Close()
	}
	if c, err := tls.Dial("tcp", addr, share.Client(share.Secret())); err == nil {
		c.Close()
		t.Error("the guest trusted a certificate with another fingerprint")
	}
}
