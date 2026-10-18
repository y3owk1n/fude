// Package tomlerr says where in a TOML file a decoding error is.
package tomlerr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Where returns err, from decoding the file at path, with the line it is on
// and the key it is about: an unknown key, a bad value or a syntax error.
func Where(path string, err error) error {
	var strict *toml.StrictMissingError
	var de *toml.DecodeError
	var msg string
	switch {
	case errors.As(err, &strict):
		de = &strict.Errors[0]
		msg = "unknown key " + strings.Join(de.Key(), ".")
	case errors.As(err, &de) && len(de.Key()) > 0:
		msg = "bad value for " + strings.Join(de.Key(), ".")
	case errors.As(err, &de):
		msg = strings.TrimPrefix(de.Error(), "toml: ")
	default:
		return fmt.Errorf("%s: %w", path, err)
	}
	line, _ := de.Position()
	return fmt.Errorf("%s line %d: %s", path, line, msg)
}
