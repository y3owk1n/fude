package editor

import "strings"

// Power is how much a session may do, fixed when it is made (B243).
type Power int

const (
	Host  Power = iota // all a session may do
	Guest              // limited as B244 to B246 say
)

// refuses says whether a guest may not run the ex command name with arg,
// because it runs a program or changes what every session shares (B244).
func refuses(name, arg string) bool {
	switch name {
	case "!", "terminal", "task", "restart", "cd", "theme":
		return true
	case "read":
		return strings.HasPrefix(arg, "!")
	case "pick":
		source, _, _ := strings.Cut(arg, " ")
		return source == "tasks"
	case "grammar", "lsp", "plugin":
		return arg != ""
	}
	return false
}

// refusedPlugs are the <Plug> commands a guest may not run, as refuses
// says of ex commands (B244).
var refusedPlugs = map[string]bool{
	"restart": true, "listing.cwd": true,
	"lsp.restart": true, "lsp.stop": true, "lsp.disable": true, "lsp.enable": true,
}
