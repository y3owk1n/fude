package editor

import (
	"sync"

	"github.com/y3owk1n/fude/internal/keep"
)

// recent is the files shown last, which modules/recent keeps and the start
// screen lists (B213, B225).
var recent = sync.OnceValue(func() *keep.Recent { return keep.NewRecent(keep.RecentPath, logLine) })
