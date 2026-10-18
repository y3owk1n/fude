package editor

import (
	"errors"
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
)

// reformat applies edits for text t to b as one timeline node, and drops
// them when b has changed since t or has closed.
func (e *Session) reformat(b *buffer, t kernel.Snapshot, edits []kernel.Edit, label string) {
	if len(edits) == 0 || !slices.Contains(e.bufs, b) {
		return
	}
	if err := e.mayEdit(); err != nil {
		e.message = err.Error()
		return
	}
	if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: label, Time: e.now(), Edits: edits}); err != nil {
		if errors.Is(err, kernel.ErrStale) {
			err = errors.New("the text changed while it was formatted")
		}
		e.message = err.Error()
		return
	}
	b.head = b.text.Head()
	b.pushChange(edits[0].Start)
}
