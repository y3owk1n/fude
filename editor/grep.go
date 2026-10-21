package editor

import (
	"path/filepath"
	"strconv"
	"strings"
)

// grepSpot splits a grep match into its path, line and column, from 1,
// and text. A file finder's match is a path, with line and column 0.
func grepSpot(match string) (string, int, int, string) {
	// The colon of a Windows path's drive, as in C:, splits nothing.
	vol := filepath.VolumeName(match)
	parts := strings.SplitN(match[len(vol):], ":", 4)
	if len(parts) < 4 {
		return match, 0, 0, ""
	}
	parts[0] = vol + parts[0]
	line, err1 := strconv.Atoi(parts[1])
	col, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return match, 0, 0, ""
	}
	return parts[0], line, col, parts[3]
}
