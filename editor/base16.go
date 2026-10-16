package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// base16Line finds a base16 slot and its colour, as a stylix palette.json
// ("base0D": "6aadcf") and a Tinted Theming scheme (base0D: "#6aadcf")
// write them.
var base16Line = regexp.MustCompile(`(?i)base0([0-9a-f])"?\s*:\s*"#?([0-9a-f]{6})"`)

// expandHome returns path with a leading ~/ made the home directory.
func expandHome(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return filepath.Join(home, rest), nil
}

// readBase16 reads the colours base00 to base0F of the base16 scheme at
// path, where a leading ~/ is the home directory (B146).
func readBase16(path string) ([16]int32, error) {
	var colours [16]int32
	path, err := expandHome(path)
	if err != nil {
		return colours, fmt.Errorf("theme: %w", err)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return colours, fmt.Errorf("theme: %w", err)
	}
	var seen [16]bool
	for _, m := range base16Line.FindAllStringSubmatch(string(src), -1) {
		// The pattern takes only hex digits, so neither parse fails.
		slot, _ := strconv.ParseInt(m[1], 16, 8)
		v, _ := strconv.ParseInt(m[2], 16, 32)
		colours[slot], seen[slot] = int32(v), true
	}
	if slices.Contains(seen[:], false) {
		return colours, fmt.Errorf("theme %s: a base16 scheme needs base00 to base0F", path)
	}
	return colours, nil
}
