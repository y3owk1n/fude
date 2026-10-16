package editor

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Man returns fude's man page in roff, the sections of help with fude's
// defaults, for fude --man (B163).
func Man() (string, error) {
	e, err := open(defaults, nil)
	if err != nil {
		return "", fmt.Errorf("man page: %w", err)
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
	fmt.Fprintf(&b, ".TH FUDE 1 %q fude \"User Commands\"\n", date)
	b.WriteString(`.SH NAME
fude \- a modal text editor
.SH SYNOPSIS
.B fude
[\fB+\fIline\fR] [\fIfile\fR ...]
.br
.B fude \-\-man
.SH DESCRIPTION
fude opens each file in a buffer, or an empty buffer when none is given.
With \fB+\fR\fIline\fR, the cursor starts on that line of the first file.
Inside fude, \fB:h\fR shows this reference, the mappings config.toml adds,
and a guide to fude.
\fB\-\-man\fR prints this page.
`)
	for _, s := range e.helpSections() {
		if len(s.entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, ".SH %s\n", roff(s.title))
		if s.note != "" {
			fmt.Fprintf(&b, "%s\n", roff(s.note))
		}
		for _, en := range s.entries {
			fmt.Fprintf(&b, ".TP\n\\fB%s\\fR\n%s\n", roff(en.head), roff(en.text))
		}
	}
	b.WriteString(`.SH FILES
.TP
\fI$XDG_CONFIG_HOME/fude/config.toml\fR
The settings, options and mappings, in \fI~/.config\fR without
$XDG_CONFIG_HOME.
.TP
\fI$XDG_STATE_HOME/fude\fR
Each file's timeline and fude's log, in \fI~/.local/state\fR without
$XDG_STATE_HOME.
.TP
\fI$XDG_RUNTIME_DIR/fude.\fIpid\fI.sock\fR
The socket clients attach to, in the temporary directory without
$XDG_RUNTIME_DIR, unless the socket setting names another.
`)
	return b.String(), nil
}

// roff escapes text for a man page: backslashes and hyphens, and a dot or
// quote that starts a line, which roff reads as a request.
func roff(text string) string {
	text = strings.NewReplacer(`\`, `\e`, "-", `\-`).Replace(text)
	if strings.HasPrefix(text, ".") || strings.HasPrefix(text, "'") {
		text = `\&` + text
	}
	return text
}
