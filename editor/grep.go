package editor

import (
	"strconv"
	"strings"
)

// grepLimit is the most lines a search shows.
const grepLimit = 1000

// grepSpot splits a grep match into its path, line and column, from 1,
// and text. A file finder's match is a path, with line and column 0.
func grepSpot(match string) (string, int, int, string) {
	parts := strings.SplitN(match, ":", 4)
	if len(parts) < 4 {
		return match, 0, 0, ""
	}
	line, err1 := strconv.Atoi(parts[1])
	col, err2 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil {
		return match, 0, 0, ""
	}
	return parts[0], line, col, parts[3]
}
