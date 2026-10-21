// Package distribution is the set of modules fude runs, which the fude
// command and the tests that measure fude as it runs share.
package distribution

import (
	"path/filepath"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/modules/autopairs"
	"github.com/y3owk1n/fude/modules/blame"
	"github.com/y3owk1n/fude/modules/bufferline"
	"github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/colours"
	"github.com/y3owk1n/fude/modules/comment"
	"github.com/y3owk1n/fude/modules/completion"
	debugmod "github.com/y3owk1n/fude/modules/debug"
	diffmod "github.com/y3owk1n/fude/modules/diff"
	"github.com/y3owk1n/fude/modules/editorconfig"
	explorermod "github.com/y3owk1n/fude/modules/explorer"
	"github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/modules/flash"
	"github.com/y3owk1n/fude/modules/fold"
	"github.com/y3owk1n/fude/modules/format"
	"github.com/y3owk1n/fude/modules/git"
	"github.com/y3owk1n/fude/modules/grammars"
	"github.com/y3owk1n/fude/modules/grep"
	"github.com/y3owk1n/fude/modules/guides"
	helpmod "github.com/y3owk1n/fude/modules/help"
	"github.com/y3owk1n/fude/modules/hints"
	"github.com/y3owk1n/fude/modules/history"
	"github.com/y3owk1n/fude/modules/inlay"
	"github.com/y3owk1n/fude/modules/jump"
	"github.com/y3owk1n/fude/modules/links"
	"github.com/y3owk1n/fude/modules/lists"
	lspmod "github.com/y3owk1n/fude/modules/lsp"
	"github.com/y3owk1n/fude/modules/markdown"
	pickmod "github.com/y3owk1n/fude/modules/pick"
	"github.com/y3owk1n/fude/modules/pins"
	"github.com/y3owk1n/fude/modules/places"
	"github.com/y3owk1n/fude/modules/plug"
	"github.com/y3owk1n/fude/modules/recent"
	"github.com/y3owk1n/fude/modules/references"
	"github.com/y3owk1n/fude/modules/review"
	sessionmod "github.com/y3owk1n/fude/modules/session"
	"github.com/y3owk1n/fude/modules/sessions"
	"github.com/y3owk1n/fude/modules/shell"
	"github.com/y3owk1n/fude/modules/spell"
	startmod "github.com/y3owk1n/fude/modules/start"
	"github.com/y3owk1n/fude/modules/sticky"
	"github.com/y3owk1n/fude/modules/surround"
	"github.com/y3owk1n/fude/modules/task"
	terminalmod "github.com/y3owk1n/fude/modules/terminal"
	thememod "github.com/y3owk1n/fude/modules/theme"
	"github.com/y3owk1n/fude/modules/timeline"
	"github.com/y3owk1n/fude/modules/window"
)

// Modules returns the modules fude adds to the editor, as the fude command
// runs it (D49).
func Modules() []api.Module {
	words := ""
	if dir, err := editor.ConfigDir(); err == nil {
		words = filepath.Join(dir, "words.txt")
	}
	return []api.Module{autopairs.New(), blame.New(), bufferline.New(), buffers.New(), colours.New(), comment.New(), completion.New(), debugmod.New(), diffmod.New(), editorconfig.New(), files.New(), flash.New(), fold.New(), format.New(), git.New(), grammars.New(), grep.New(), guides.New(), helpmod.New(), hints.New(), history.New(), inlay.New(), jump.New(), links.New(), explorermod.New(), lists.New(), lspmod.New(), markdown.New(), pickmod.New(), places.New(), pins.New(), plug.New(), recent.New(), references.New(), review.New(), sessionmod.New(), sessions.New(), shell.New(), spell.New(words), startmod.New(), sticky.New(), surround.New(), task.New(), terminalmod.New(), thememod.New(), timeline.New(), window.New()}
}
