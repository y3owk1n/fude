# Two people in one fude: ana shares, ben joins and she lets him in, they
# edit the same buffer at once, ben's u undoes only his own edit, and ben
# follows ana, into a tab page of her own. Both run on this machine, in two
# tmux panes.
Output demos/together.gif
Output demos/out/together.mp4
Source demos/style.tape
Set FontSize 14
# Two panes side by side need the width, so each status line shows whole.
Set Width 2100

Hide
Type "source demos/setup.sh"
Enter
Wait
Type `tmux -L fude-demo -f $repo/demos/tmux.conf new-session -e USER=ana \; select-pane -T ana \; split-window -h -e USER=ben \; select-pane -T ben \; select-pane -L`
Enter
Sleep 2s
Show
Sleep 500ms

# ana shares a file. The share string goes to the clipboard.
Type "fude share 127.0.0.1:7419 protocol/framing/framing.go"
Enter
Wait+Screen@30s /guests join with/
Sleep 2s

# ben pastes it in his own terminal, and ana lets him in.
Ctrl+B
Right
Sleep 500ms
Ctrl+V
Sleep 800ms
Enter
Wait+Screen@30s /wants to join/
Sleep 1s
Ctrl+B
Left
Sleep 500ms
Type "y"
Sleep 2s

# They edit at once, each cursor in its own colour on the other's screen.
Ctrl+B
Right
Sleep 300ms
Type "/no Content"
Enter
Sleep 300ms
Type@90ms `ci"no Content-Length header`
Escape
Sleep 800ms
Ctrl+B
Left
Sleep 300ms
Type "/bad Content"
Enter
Sleep 300ms
Type@90ms `ci"Content-Length %q is not a number`
Escape
Sleep 1.5s

# ben's u undoes his edit alone, and ana's stays.
Ctrl+B
Right
Sleep 300ms
Type "u"
Sleep 2s

# ben follows ana, and his window goes where hers does, into a new tab
# page and back.
Type ":follow ana"
Enter
Sleep 1s
Ctrl+B
Left
Sleep 300ms
Type "/Lines struct"
Enter
Sleep 1.5s
Type "G"
Sleep 1.5s
Type "gg"
Sleep 1.5s
Type ":tabe protocol/framing/framing_test.go"
Enter
Sleep 2s
Type "gt"
Sleep 2s

Hide
Type ":qa!"
Enter
Sleep 500ms
Type "tmux -L fude-demo kill-server"
Enter
