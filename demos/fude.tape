# One pass through fude on its own source: key hints, the finder, live grep,
# hover, a tree-sitter edit and its undo, completion and the timeline.
Output demos/fude.gif
Output demos/out/fude.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh"
Enter
Wait
Show
Sleep 500ms

Type "fude editor/files.go"
Enter
# gopls loads the workspace.
Sleep 3s

# <Space> waits, and key hints show what may follow.
Type " "
Sleep 1.5s
Type " "
Sleep 700ms
Type@120ms "termthm"
Sleep 1.5s
Enter
Sleep 1.5s

# Live grep, grouped by file.
Type " sg"
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

# Completion, signature help and autopairs.
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
Sleep 500ms

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
