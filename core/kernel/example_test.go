package kernel_test

import (
	"fmt"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
)

func Example() {
	b := kernel.New("hello world", nil)
	world := b.SetMark(6, kernel.Left)
	t := kernel.Transaction{Base: b.Snapshot().Version(), Origin: "me", Label: "cw", Time: time.Now()}
	t.Edits = []kernel.Edit{{Start: 0, End: 5, Text: "goodbye"}}
	if err := b.Apply(t); err != nil {
		fmt.Println(err)
	}
	show := func(what string) {
		s := b.Snapshot()
		fmt.Printf("%-6s %q, world at %d\n", what, s.Slice(0, s.Len()), b.Mark(world))
	}
	show("apply")
	b.Undo()
	show("undo")
	b.Redo()
	show("redo")
	fmt.Println(len(b.Nodes()), "node on the timeline")
	// Output:
	// apply  "goodbye world", world at 8
	// undo   "hello world", world at 6
	// redo   "goodbye world", world at 8
	// 1 node on the timeline
}
