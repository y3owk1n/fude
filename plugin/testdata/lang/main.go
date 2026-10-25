// Package main is a test plugin written with the sdk. It names a grammar
// for the filetype its option filetype says, from the repo and commit its
// options give, with Go's highlights from the repo and a folds query as
// text. Needs grammar.
package main

import (
	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/sdk"
)

func init() {
	sdk.Define("filetype", "gox", "the filetype the grammar is for")
	sdk.Define("repo", "", "the grammar's git repo")
	sdk.Define("commit", "", "the commit to build")
	ft, _ := sdk.Option("lang.filetype")
	repo, _ := sdk.Option("lang.repo")
	commit, _ := sdk.Option("lang.commit")
	sdk.Register(api.Registration{Kind: api.Language, Name: ft, Source: &api.GrammarSource{
		Repo: repo, Commit: commit,
		Files:   map[string]string{"highlights": "queries/highlights.scm"},
		Queries: map[string]string{"folds": "(function_declaration) @fold"},
	}}, func(_, _, _ int) int { return 0 })
}

func main() {}
