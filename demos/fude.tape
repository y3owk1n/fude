# One short pass through fude on its own source: the start screen, the
# finder, key hints, live grep, hover, a tree-sitter edit and its undo,
# completion, editing every match at once and the timeline.
Output demos/fude.gif
Output demos/out/fude.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh"
Enter
Wait
# Go's grammar builds before the clip, as it would on first use.
Type "fude"
Enter
Sleep 1s
Type ":grammar install go"
Enter
Wait+Screen@180s /built the go grammar|up to date|is built/
Type ":qa!"
Enter
Wait
Type "clear"
Enter
Wait
Show
Sleep 500ms

# The start screen, then the finder.
Type "fude"
Enter
Sleep 2s
Type "f"
Sleep 700ms
Type@120ms "files.go"
Sleep 1.2s
Enter
# gopls loads the workspace.
Sleep 3s

# <Space> waits, and key hints show what may follow.
Type " "
Sleep 1.5s
Type "sg"
Sleep 400ms
Type@100ms "writePath("
Sleep 1.5s
Ctrl+N
Sleep 600ms
Ctrl+N
Sleep 900ms
Enter
Sleep 1.2s

# Hover from the language server.
Type "/Cut("
Enter
Sleep 400ms
Type "K"
Sleep 2.5s
Escape
Sleep 400ms

# A function as a tree-sitter object: select it, delete it, undo.
Type "/func shorten"
Enter
Sleep 500ms
Type "vaf"
Sleep 1.5s
Escape
Type "daf"
Sleep 1.2s
Type "u"
Sleep 1.5s

# Completion, signature help, autopairs and inlay hints.
Type "/Cut("
Enter
Sleep 300ms
Type@90ms "o_ = strings.Rep"
Sleep 1.5s
Ctrl+N
Sleep 1s
Type@120ms `"-", 3`
Sleep 1.5s
Escape
Sleep 800ms

# <C-n> selects each match of the word, and one change edits them all.
Type "/room :="
Enter
Sleep 400ms
Ctrl+N
Sleep 300ms
Ctrl+N
Sleep 300ms
Ctrl+N
Sleep 1.2s
Type@120ms "cspace"
Sleep 1.2s
Escape
Sleep 800ms
Type "u"
Sleep 600ms

# Every state, on a branching timeline. d diffs one against now.
Type ":timeline"
Enter
Sleep 1.5s
Type "k"
Sleep 600ms
Type "d"
Sleep 2.5s

Hide
Type ":qa!"
Enter
