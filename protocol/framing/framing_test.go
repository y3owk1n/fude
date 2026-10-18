package framing_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/y3owk1n/fude/protocol/framing"
)

// B60, B261: both protocols' messages travel in Content-Length frames.
func TestFramesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	for _, body := range []string{`{"id":1}`, `{"method":"x","params":"ü"}`} {
		if err := framing.Write(&buf, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	in := bufio.NewReader(&buf)
	for _, want := range []string{`{"id":1}`, `{"method":"x","params":"ü"}`} {
		got, err := framing.Read(in)
		if err != nil || string(got) != want {
			t.Fatalf("read %q, %v, want %q", got, err, want)
		}
	}
	if _, err := framing.Read(in); err == nil {
		t.Error("a read past the last frame gave no error")
	}
}
