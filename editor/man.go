package editor

import (
	"cmp"
	"fmt"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Subcommand is one of fude's subcommands, as fude help and fude(1) list
// it (B348, D52).
type Subcommand struct{ Name, Args, Help string }

// Subcommands are fude's subcommands.
var Subcommands = []Subcommand{
	{"open", "<file>...", "open the files in a running fude, as :e does"},
	{"keys", "<keys>", "type keys into a running fude, as if typed there"},
	{"state", "", "print a running fude's buffer, cursor and mode as JSON"},
	{"attach", "", "open a terminal on a new session of a running fude"},
	{"tail", "", "print what a running fude does, as it happens"},
	{"share", "[<address>] [<file>...]", "open the files and let a guest join"},
	{"join", "<string>", "join the fude that shared string"},
	{"relay", "[<address>]", "pass shares through, on port 7418 without an address"},
	{"plugin new", "<name>", "write a plugin in Go in a new folder"},
	{"plugin build", "[<dir>]", "build the plugin in dir with TinyGo"},
	{"man", "[config|guide]", "print fude(1), fude-config(5) or fude-guide(7)"},
	{"help", "", "print this help"},
	{"version", "", "print the date and commit fude was built from"},
}

// Flag is one of fude's flags, with the subcommands that take it, fude for
// opening files. A flag followed by <word> takes a value.
type Flag struct {
	Flag, Help string
	For        []string
}

// Flags are fude's flags.
var Flags = []Flag{
	{"--clean", "read no config", []string{"fude", "share", "join"}},
	{"--name <name>", "the name others see you by", []string{"fude", "attach", "share", "join"}},
	{"--socket <path>", "the fude to reach", []string{"open", "keys", "state", "attach", "tail"}},
	{"--session <name>", "the session to act on", []string{"open", "keys", "state"}},
	{"--relay <host>", "share through the relay at host", []string{"share"}},
	{"--read-only", "join only to watch", []string{"join"}},
	{"--json", "print each event as JSON", []string{"tail"}},
	{"-h, --help", "print this help", nil},
	{"-v, --version", "print the version", nil},
}

// usageWord is a word of a usage line: a <word> to fill in, or one typed
// as it is.
var usageWord = regexp.MustCompile(`<[^>]+>|[^\s\[\]|<>.,]+`)

// usageRoff is a usage line in roff, its <words> italic and the rest bold.
func usageRoff(line string) string {
	return usageWord.ReplaceAllStringFunc(escape(line), func(w string) string {
		if fill, ok := strings.CutPrefix(w, "<"); ok {
			return `\fI` + strings.TrimSuffix(fill, ">") + `\fR`
		}
		return `\fB` + w + `\fR`
	})
}

// Man returns the man page that page names in roff, from help with fude's
// defaults and the modules the distribution adds, for fude man (B163).
func Man(page string, modules ...api.Module) (string, error) {
	e, err := open(defaults, nil)
	if err != nil {
		return "", fmt.Errorf("man page: %w", err)
	}
	for _, m := range modules {
		e.Use(m)
	}
	// The date of the commit fude was built from, when the build knows it.
	date := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" && len(s.Value) >= 10 {
				date = s.Value[:10]
			}
		}
	}
	var b strings.Builder
	// The options and settings are fude-config(5)'s, and the rest of the
	// reference is fude(1)'s.
	config := func(s helpSection) bool { return s.title == "OPTIONS" || s.title == "SETTINGS" }
	more := details()
	reference := func(keep func(helpSection) bool) {
		for _, s := range e.helpSections() {
			if len(s.entries) == 0 || !keep(s) {
				continue
			}
			fmt.Fprintf(&b, ".SH %s\n", roff(s.title))
			if s.note != "" {
				fmt.Fprintf(&b, "%s\n", roff(s.note))
			}
			for _, en := range s.entries {
				fmt.Fprintf(&b, ".TP\n\\fB%s\\fR\n%s\n", roff(en.head), roff(en.text))
				// The longer explanation's paragraphs stay under the entry.
				if d := more[en.tags[0]]; d != "" {
					b.WriteString(".RS\n.PP\n" + manText(d, "") + ".RE\n")
				}
			}
		}
	}
	pages := guide()
	switch page {
	case "":
		fmt.Fprintf(&b, ".TH FUDE 1 %q fude \"User Commands\"\n", date)
		// The subcommands and flags are fude help's (B348).
		commands := ""
		for _, c := range Subcommands {
			commands += ".TP\n" + usageRoff("fude "+c.Name+" "+c.Args) + "\n" + roff(c.Help) + "\n"
		}
		commands += ".SH FLAGS\n"
		for _, f := range Flags {
			commands += ".TP\n" + usageRoff(f.Flag) + "\n" + roff(f.Help)
			if f.For != nil {
				commands += roff(", for " + strings.Join(f.For, ", "))
			}
			commands += "\n"
		}
		b.WriteString(`.SH NAME
fude \- a modal text editor
.SH SYNOPSIS
` + usageRoff("fude [<flags>] [+<line>] [<file>...]") + `
.br
` + usageRoff("fude <command> [<flags>] [<args>]") + `
.SH DESCRIPTION
fude opens each file in a buffer, or an empty buffer when none is given.
A folder opens as a listing of its entries.
With \fB+\fR\fIline\fR, the cursor starts on that line of the first file.
Inside fude, \fB:h\fR shows this reference, the settings, the mappings
config.toml adds and the guide.
.SH SUBCOMMANDS
` + commands)
		reference(func(s helpSection) bool { return !config(s) })
		b.WriteString(`.SH ENVIRONMENT
.TP
\fBFUDE_SOCKET\fR
The socket that \fBopen\fR, \fBkeys\fR, \fBstate\fR, \fBattach\fR and
\fBtail\fR reach without \fB\-\-socket\fR. Without it they reach the socket
setting's, or else the one fude running. \fB:terminal\fR sets it to where
fude listens.
.TP
\fBFUDE_SESSION\fR
The session that \fBopen\fR, \fBkeys\fR and \fBstate\fR act on without
\fB\-\-session\fR. Without it they act on the session of fude's own
terminal. \fB:terminal\fR sets it to the session it runs in.
.SH FILES
.TP
\fI$XDG_CONFIG_HOME/fude/config.toml\fR
The settings, options and mappings, in \fI~/.config\fR without
$XDG_CONFIG_HOME. See fude\-config(5).
.TP
\fI$XDG_STATE_HOME/fude\fR
Each file's timeline and fude's log, in \fI~/.local/state\fR without
$XDG_STATE_HOME.
.TP
\fI$XDG_DATA_HOME/fude/grammars\fR
The grammars fude builds, in \fI~/.local/share\fR without $XDG_DATA_HOME.
.TP
\fI$XDG_RUNTIME_DIR/fude.\fIpid\fI.sock\fR
The socket clients attach to, in the temporary directory without
$XDG_RUNTIME_DIR, unless the socket setting names another.
.SH SEE ALSO
fude\-config(5), fude\-guide(7)
`)
	case "config":
		fmt.Fprintf(&b, ".TH FUDE-CONFIG 5 %q fude \"File Formats\"\n", date)
		b.WriteString(".SH NAME\nfude\\-config \\- fude's config.toml\n")
		// The guide's page on config is the description.
		i := slices.IndexFunc(pages, func(p guidePage) bool { return p.tag == "guide-config" })
		b.WriteString(manText(pages[i].text, ".SH DESCRIPTION"))
		reference(config)
		b.WriteString(".SH SEE ALSO\nfude(1), fude\\-guide(7)\n")
	case "guide":
		fmt.Fprintf(&b, ".TH FUDE-GUIDE 7 %q fude \"Miscellaneous\"\n", date)
		b.WriteString(".SH NAME\nfude\\-guide \\- a guide to the fude editor\n")
		for _, p := range pages {
			b.WriteString(manText(p.text, ""))
		}
		b.WriteString(".SH SEE ALSO\nfude(1), fude\\-config(5)\n")
	default:
		return "", fmt.Errorf("no man page %q, only config and guide", page)
	}
	return b.String(), nil
}

// manText returns a guide page's markdown as roff. Its title becomes a
// section, or head when head is set, and its headings subsections. It
// reads paragraphs, "- " lists, fenced code and inline code.
func manText(md, head string) string {
	var b strings.Builder
	// A heading starts a paragraph of its own, so no .PP follows one.
	heading := false
	para := func() {
		if !heading {
			b.WriteString(".PP\n")
		}
	}
	lines := strings.Split(md, "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "# "):
			b.WriteString(cmp.Or(head, ".SH "+roff(strings.ToUpper(strings.TrimPrefix(l, "# ")))) + "\n")
			heading = true
		case strings.HasPrefix(l, "## "):
			b.WriteString(".SS " + roff(strings.TrimPrefix(l, "## ")) + "\n")
			heading = true
		case strings.HasPrefix(l, "```"):
			para()
			b.WriteString(".RS 4\n.nf\n")
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
				b.WriteString(roff(lines[i]) + "\n")
			}
			b.WriteString(".fi\n.RE\n")
			heading = false
		case strings.HasPrefix(l, "- "):
			b.WriteString(".IP \\(bu 2\n" + manInline(strings.TrimPrefix(l, "- ")) + "\n")
			// A list item goes on over the lines indented under it.
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], "  ") {
				i++
				b.WriteString(manInline(strings.TrimSpace(lines[i])) + "\n")
			}
			heading = false
		case l == "":
			// A blank line before text starts a paragraph.
			if next := lines[min(i+1, len(lines)-1)]; next != "" && !strings.HasPrefix(next, "#") && !strings.HasPrefix(next, "- ") && !strings.HasPrefix(next, "```") {
				para()
			}
		default:
			b.WriteString(manInline(l) + "\n")
			heading = false
		}
	}
	return b.String()
}

// codeSpan is markdown's inline code, in single backticks or in double
// ones around text with a backtick.
var codeSpan = regexp.MustCompile("``\\s?(.+?)\\s?``|`([^`]+)`")

// manInline returns a line of markdown as roff, with its inline code in bold.
func manInline(text string) string {
	var b strings.Builder
	at := 0
	for _, m := range codeSpan.FindAllStringSubmatchIndex(text, -1) {
		b.WriteString(escape(text[at:m[0]]))
		// The first group is a double backtick span's, the second a single's.
		code := ""
		if m[2] >= 0 {
			code = text[m[2]:m[3]]
		} else {
			code = text[m[4]:m[5]]
		}
		b.WriteString(`\fB` + escape(code) + `\fR`)
		at = m[1]
	}
	b.WriteString(escape(text[at:]))
	return guard(b.String())
}

// roff escapes a line of text for a man page.
func roff(text string) string { return guard(escape(text)) }

// escape escapes the backslashes and hyphens of text for roff.
func escape(text string) string {
	return strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(text)
}

// guard keeps roff from reading a line that starts with a dot or a quote as
// a request.
func guard(line string) string {
	if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
		return `\&` + line
	}
	return line
}
