package lsp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// tree is the call or type hierarchy asked for: what the key lists, and
// the items the server prepared under the cursor (B464, B465).
type tree struct {
	method, what string
	items        []json.RawMessage
	server       string
}

// trees are each hierarchy key's request for the places it lists, and
// their name in a message.
var trees = map[uint32][2]string{
	incomingCalls: {"callHierarchy/incomingCalls", "incoming calls"},
	outgoingCalls: {"callHierarchy/outgoingCalls", "outgoing calls"},
	supertypes:    {"typeHierarchy/supertypes", "supertypes"},
	subtypes:      {"typeHierarchy/subtypes", "subtypes"},
}

// item is an item of a call or type hierarchy.
type item struct {
	Name   string    `json:"name"`
	Detail string    `json:"detail"`
	URI    string    `json:"uri"`
	Range  lsp.Range `json:"range"`
}

// prepare asks for the hierarchy items under the cursor, for the key id.
func (t *tree) prepare(id uint32, cursor int) api.Result {
	t.method, t.what = trees[id][0], trees[id][1]
	prepare := "textDocument/prepareCallHierarchy"
	if strings.HasPrefix(t.method, "type") {
		prepare = "textDocument/prepareTypeHierarchy"
	}
	return api.Result{Requests: []api.Request{{ID: prepared, Method: prepare, Pos: cursor, End: -1}}}
}

// answered asks for the places of the one item prepared, or offers several
// in a menu, and lists the places as references are.
func (t *tree) answered(id uint32, s api.Scope) api.Result {
	switch err := s.About["error"]; {
	case strings.HasPrefix(err, "no language server answered"):
		return api.Result{Error: "no " + t.what + " found"}
	case err != "":
		return api.Result{Error: offers(err, t.what)}
	}
	switch id {
	case prepared:
		json.Unmarshal([]byte(s.About["output"]), &t.items)
		t.server = s.About["server"]
		switch len(t.items) {
		case 0:
			return api.Result{Error: "no " + t.what + " found"}
		case 1:
			return t.ask(0)
		}
		menu := make([]api.MenuItem, len(t.items))
		for i, raw := range t.items {
			var it item
			json.Unmarshal(raw, &it)
			menu[i] = api.MenuItem{Title: strings.TrimSpace(it.Name + " " + it.Detail)}
		}
		return api.Result{Menu: &api.Menu{ID: itemPick, Items: menu}}
	case itemPick:
		i, _ := strconv.Atoi(s.About["picked"])
		if s.About["ok"] != "true" || i < 0 || i >= len(t.items) {
			return api.Result{}
		}
		return t.ask(i)
	}
	// An incoming call is in its caller's file, an outgoing one in the
	// item's, and a type is in its own.
	var found []struct {
		item
		From       *item       `json:"from"`
		To         *item       `json:"to"`
		FromRanges []lsp.Range `json:"fromRanges"`
	}
	json.Unmarshal([]byte(s.About["output"]), &found)
	var own item
	json.Unmarshal(t.items[0], &own)
	var places []string
	add := func(uri string, at lsp.Position, text string) {
		if path, ok := lsp.Path(uri); ok {
			places = append(places, fmt.Sprintf("%s:%d:%d:%s", relative(path), at.Line+1, at.Character+1, text))
		}
	}
	for _, f := range found {
		switch {
		case f.From != nil:
			for _, r := range f.FromRanges {
				add(f.From.URI, r.Start, f.From.Name)
			}
		case f.To != nil:
			for _, r := range f.FromRanges {
				add(own.URI, r.Start, f.To.Name)
			}
		default:
			add(f.URI, f.Range.Start, strings.TrimSpace(f.Name+" "+f.Detail))
		}
	}
	if len(places) == 0 {
		return api.Result{Error: "no " + t.what + " found"}
	}
	return api.Result{Places: &api.Places{Title: t.what, Items: places, Show: true, Encoding: s.About["encoding"]}}
}

// ask asks the server that prepared the items for the places of item i.
func (t *tree) ask(i int) api.Result {
	t.items = t.items[i : i+1]
	params, _ := json.Marshal(map[string]json.RawMessage{"item": t.items[0]})
	return api.Result{Requests: []api.Request{{ID: hierarchy, Method: t.method, Pos: -1, End: -1, Params: string(params), Server: t.server}}}
}
