// Package main is a test plugin written with the sdk.
// init registers the completion source words, which offers fude and fudge.
// Needs buffer.read and ui.
package main

import (
	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Register(api.Registration{Kind: api.Completer, Name: "words", Help: "offers fude and fudge"}, func(_, _, _ int) int {
		err := sdk.Do(api.Result{Completion: &api.Completion{Items: []api.CompletionItem{
			{Label: "fude", Text: "fude"},
			{Label: "fudge", Text: "fudge"},
		}}})
		if err != nil {
			sdk.Log(err.Error())
		}
		return 0
	})
}

func main() {}
