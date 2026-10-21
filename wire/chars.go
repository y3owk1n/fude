package wire

// ListChars are what list shows in place of tabs, blanks and no-break
// spaces, and at a line's ends, as config.toml's [list_chars] sets them.
// "" shows the text as it is (B486).
type ListChars struct {
	Tab      string `toml:"tab"`      // a tab's first cell, the cells after it and, given a third, its last
	Space    string `toml:"space"`    // a blank
	Lead     string `toml:"lead"`     // a blank before a line's text, in place of Space
	Trail    string `toml:"trail"`    // a blank after a line's text, in place of Space
	Nbsp     string `toml:"nbsp"`     // a no-break space
	Eol      string `toml:"eol"`      // after a line's end
	Extends  string `toml:"extends"`  // without wrap, the last column of a line that goes on past it
	Precedes string `toml:"precedes"` // without wrap, the first column of a line that starts before it
}

// FillChars are what fills where the screen shows no text, as config.toml's
// [fill_chars] sets them (B487).
type FillChars struct {
	Eob       string `toml:"eob"`        // the rows past the end of the text
	Vert      string `toml:"vert"`       // the line between windows side by side
	Fold      string `toml:"fold"`       // the rest of a closed fold's row
	FoldOpen  string `toml:"fold_open"`  // in the fold column, a line that starts an open fold (B488)
	FoldClose string `toml:"fold_close"` // in the fold column, a closed fold
	FoldSep   string `toml:"fold_sep"`   // in the fold column, the other lines a fold holds
}
