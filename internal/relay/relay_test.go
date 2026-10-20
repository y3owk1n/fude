package relay_test

import (
	"bufio"
	"io"
	"net"
	"testing"
	"time"

	"github.com/y3owk1n/fude/internal/relay"
)

// B251, bytes a guest sends as soon as it joins reach the host, even when
// they come with the relay's line that a guest took the place.
func TestB251PassesEarlyBytes(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go relay.Serve(l)
	p, err := relay.Offer(l.Addr().String(), "room")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	// The guest's hello and its first bytes go in one write, so the relay
	// reads them together.
	g, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if _, err := g.Write([]byte("{\"room\":\"room\",\"role\":\"join\"}\nhello")); err != nil {
		t.Fatal(err)
	}
	read(t, p)

	// A relay that sends its line and the guest's bytes in one write makes
	// the host read them together.
	fake, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	go func() {
		c, err := fake.Accept()
		if err != nil {
			return
		}
		bufio.NewReader(c).ReadString('\n')
		c.Write([]byte("{}\n"))
		c.Write([]byte("{}\nhello"))
	}()
	p, err = relay.Offer(fake.Addr().String(), "room")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	read(t, p)
}

// read takes the host's place p and reads hello from the guest.
func read(t *testing.T, p *relay.Place) {
	t.Helper()
	c, err := p.Take()
	if err != nil {
		t.Fatal(err)
	}
	// The deadline only turns a hang into a failure.
	c.SetReadDeadline(time.Now().Add(30 * time.Second))
	got := make([]byte, 5)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "hello" {
		t.Errorf("the host read %q and %v, want hello", got, err)
	}
}
