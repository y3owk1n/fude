// Package split says where a split opens, as split_below and split_right
// set (N16).
package split

import "github.com/y3owk1n/fude/api"

// Way is where a split opens: below, or right when across, or above or
// left with split_below or split_right off.
func Way(s api.Scope, across bool) string {
	if across {
		if on, _ := s.Option("split_right"); on == "false" {
			return "left"
		}
		return "right"
	}
	if on, _ := s.Option("split_below"); on == "false" {
		return "above"
	}
	return "below"
}
