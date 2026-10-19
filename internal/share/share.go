// Package share is what a host and a guest agree on to share a fude over
// TLS: a self-signed certificate the share string names by fingerprint,
// the string itself, and where a host is reached (B248, B249).
package share

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// Server returns the TLS a host serves guests with, on a new self-signed
// certificate, and the certificate's fingerprint, which its share strings
// carry.
func Server() (*tls.Config, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", err
	}
	template := &x509.Certificate{SerialNumber: serial, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, "", err
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}, fingerprint(der), nil
}

// Client returns the TLS a guest dials a host with. It trusts the one
// certificate whose fingerprint is print, and no other.
func Client(print string) *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13, VerifyConnection: func(cs tls.ConnectionState) error {
		if fingerprint(cs.PeerCertificates[0].Raw) != print {
			return errors.New("its certificate is not the one the share string names")
		}
		return nil
	}}
}

// fingerprint is the SHA-256 of a certificate, base64url.
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Secret returns a new random secret, as a share string's or a relay room's.
func Secret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Invite is a share string: where the host is reached, its certificate's
// fingerprint and the secret that lets one guest in (B249).
type Invite struct {
	Addr, Print, Secret string
}

// String is the invite as the host sends it, addr/print/secret.
func (i Invite) String() string { return i.Addr + "/" + i.Print + "/" + i.Secret }

// Parse reads a share string.
func Parse(s string) (Invite, error) {
	i := strings.LastIndexByte(s, '/')
	j := strings.LastIndexByte(s[:max(i, 0)], '/')
	if j < 0 {
		return Invite{}, fmt.Errorf("%q is not a share string", s)
	}
	return Invite{Addr: s[:j], Print: s[j+1 : i], Secret: s[i+1:]}, nil
}

// Reachable returns where a guest reaches the listener at a, which listens
// on address: its host, or else each IPv4 address of this machine that is
// neither loopback nor link-local, joined by commas, for the guest to race
// (B248).
func Reachable(a *net.TCPAddr, address string) (string, error) {
	port := fmt.Sprint(a.Port)
	if host, _, err := net.SplitHostPort(address); err == nil && host != "" && !net.ParseIP(host).IsUnspecified() {
		return net.JoinHostPort(host, port), nil
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}
	var reach []string
	for _, ad := range addrs {
		if ip, ok := ad.(*net.IPNet); ok && ip.IP.To4() != nil && ip.IP.IsGlobalUnicast() {
			reach = append(reach, net.JoinHostPort(ip.IP.String(), port))
		}
	}
	if len(reach) == 0 {
		return "", errors.New("this machine has no address but loopback, so name one")
	}
	return strings.Join(reach, ","), nil
}
