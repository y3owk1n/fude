# Two edits, then fude dies before a save. The file on disk is as it was.
# Opening it again offers the edits back, :recover restores them, and the
# timeline lists each one.
Output demos/recover.gif
Output demos/out/recover.mp4
Source demos/style.tape

Hide
Type "source demos/setup.sh"
Enter
Wait
Show
Sleep 500ms

# Two edits, not saved.
Type "fude protocol/framing/framing.go"
Enter
Sleep 2s
Type "/bad Content"
Enter
Sleep 300ms
Type@90ms `ci"Content-Length %q is not a number`
Escape
Sleep 800ms
Type "/no Content"
Enter
Sleep 300ms
Type@90ms `ci"no Content-Length header`
Escape
Sleep 1.2s

# fude's own terminal kills fude, as a crash would.
Type ":terminal"
Enter
Sleep 1s
Type "kill -9 $PPID"
Sleep 600ms
Enter
Wait
Sleep 1s

# The kill leaves the terminal as fude had it.
Hide
Type "reset; clear"
Enter
Wait
Show

# Neither edit reached the disk.
Type "grep -c 'not a number' protocol/framing/framing.go"
Enter
Wait
Sleep 1.5s

# Opening the file again offers the edits, and :recover brings them back.
Type "fude protocol/framing/framing.go"
Enter
Sleep 3s
Type ":recover"
Sleep 400ms
Enter
Sleep 2s

# Each edit is on the timeline, and d diffs the text before them with now.
Type ":timeline"
Enter
Sleep 1.5s
Type "gg"
Sleep 500ms
Type "d"
Sleep 3s

Hide
Type ":qa!"
Enter
