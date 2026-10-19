package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// program is SHELL when Windows can run it, or else PowerShell 7, or else
// Windows PowerShell. Git Bash sets SHELL to /usr/bin/bash, which Windows
// cannot run.
func program() string {
	for _, name := range []string{os.Getenv("SHELL"), "pwsh", "powershell"} {
		if name == "" {
			continue
		}
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "powershell"
}

// utf8 makes PowerShell write UTF-8 to fude. Without it, PowerShell writes
// in the console's old code page.
const utf8 = "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); "

// run is what the shell takes to run cmd. PowerShell skips its profile, as
// sh -c reads no startup file.
func run(sh, cmd string) []string {
	switch strings.ToLower(strings.TrimSuffix(filepath.Base(sh), filepath.Ext(sh))) {
	case "pwsh", "powershell":
		return []string{"-NoProfile", "-Command", utf8 + cmd}
	case "cmd":
		return []string{"/C", cmd}
	}
	return []string{"-c", cmd}
}
