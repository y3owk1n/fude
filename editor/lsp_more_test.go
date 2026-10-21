package editor_test

import "testing"

// B470
func TestB470PulledDiagnostics(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "diagnostic")
	t.Setenv("FUDE_FAKE_SYNC", `{"change":1,"save":{}}`)
	e, _ := withServer(t, "worse\n", "utf-8")
	errors := func(n int) func() bool { return func() bool { return e.Windows()[0].Errors == n } }
	settle(t, e, errors(1))
	// fude asks again after a change.
	feed(e, "dd")
	settle(t, e, errors(0))
	// The server's diagnostics change with each write, and it asks fude to
	// ask again.
	feed(e, ":w<CR>")
	settle(t, e, errors(1))
}
