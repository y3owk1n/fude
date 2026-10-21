# Text objects

A text object selects text around the cursor, wherever in it the cursor
is. It works after an operator, as `diw` deletes the word, and in visual
mode, where it selects what it covers at the cursor. An `i` object takes the text inside, and an `a` object takes the
pair or the blanks around it too. n is the count typed before it, or 1
without one. A count after the operator works as well, so `d2aw` and
`2daw` both delete two words.

An object that finds nothing fails, so the operator does nothing. Most
select characters. `ip`, `ap`, `al` and `gc` select whole lines, as does
an `i` bracket object whose brackets sit on lines of their own. In visual
mode such an object selects lines as `V` does. Typing an object again in
visual mode grows the selection. A word, sentence or paragraph object adds
the next one after it, so `vawaw` selects two words. A bracket, quote or
tag object takes the next pair out, so `vi(i(` selects inside the
parentheses around those, and `vi"i"` takes the quotes too.

```keys
"foo |bar baz qux" "vawawd" "foo |qux"
"f(a(|bb)c)" "vi(i(d" "f(|)"
"\"a |b\"" "vi\"i\"d" "|"
```

The syntax objects, `af` to `[n` at the end, come from the grammar of the
file's filetype. With no grammar built for it, or before its first parse,
they find nothing. When the selection is already what `af`, `if`, `ac`,
`ic`, `aa` or `ia` selects, it takes the next larger one, so a count, or
the object again in visual mode, grows the selection.

## a"

The string in double quotes around the cursor, the quotes and the blanks
after it, or the blanks before it when none follow. It looks on the
cursor's line only, and a quote after a backslash does not count. On a
quote, the quotes pair from the start of the line. Off one, it takes the
quotes around the cursor, or the first string after it on the line.

```keys
"x = |\"ab\" + y" "da\"" "x = |+ y"
"x = \"a|b\"" "da\"" "x |="
"|say \"hi\" now" "da\"" "say |now"
```

## a'

The string in single quotes, as `a"` takes one in double quotes.

```keys
"x 'a|b' y" "da'" "x |y"
```

## a(

The parentheses around the cursor and the text inside, or the n-th pair
out. A `(` under the cursor counts as the first. Off any pair, it takes
the first pair after the cursor on its line. It may span lines.
`a)` is the same.

```keys
"x = f(a, |b)" "da(" "x = |f"
"x = f(a, (|b))" "2da(" "x = |f"
"|f(a) x" "da(" "f| x"
```

## a<

The angle brackets around the cursor and the text inside, as `a(` takes
parentheses. `a>` is the same.

```keys
"Vec<|int> x" "da<lt>" "Vec| x"
```

## a[

The square brackets around the cursor and the text inside, as `a(` takes
parentheses. `a]` is the same.

```keys
"x[|i] = 1" "da]" "x| = 1"
```

## a`

The string in backticks, as `a"` takes one in double quotes.

```keys
"run `l|s` now" "da`" "run |now"
```

## aB

The same as `a{`.

## ab

The same as `a(`.

## al

Every line of the text. `dal` empties the buffer and `yal` copies all of
it. A count changes nothing.

```keys
"a\nb|c\nd" "dal" "|"
```

## ap

The paragraph and the blank lines after it, n times, or the blank lines
before it when it ends the text. A line of only blanks counts as blank.
On blank lines it takes them and the paragraph after. Linewise.

```keys
"|a\nb\n\nc" "dap" "|c"
"a\n\n|b\nc" "dap" "|a"
"|a\n\nb\n\nc" "d2ap" "|c"
```

## as

The sentence and the blanks after it, or the blanks before it when it is
the paragraph's last, n times. A sentence ends as `)` finds the end.
When the text ends a line and starts at or before its first non-blank,
it takes whole lines.

```keys
"One. T|wo. Three." "das" "One. |Three."
"One. Tw|o." "das" "One|."
```

## at

The n-th pair of tags of one name around the cursor, from the `<` of the
opening tag to the `>` of the closing one. It reads the whole text, so a
pair may span lines. A tag that closes itself, as `<br/>`, and a comment
pair with nothing.

```keys
"<p>a <b>|x</b></p>" "dat" "<p>a |</p>"
"<p>a <b>|x</b></p>" "d2at" "|"
```

## aW

n runs of non-blanks, each with the blanks after it, as `aw` takes words.

```keys
"foo |a.b bar" "daW" "foo |bar"
```

## aw

n words, each with the blanks after it. At the line's end, with no blanks
after, it takes the blanks before. On blanks, it takes them and the word
after. It stays on the cursor's line, and fails on an empty line or when
the line has fewer than n words left.

```keys
"foo |bar baz" "daw" "foo |baz"
"foo |bar" "daw" "fo|o"
"|a b c d" "d2aw" "|c d"
"foo|  bar baz" "daw" "foo| baz"
```

## a{

The braces around the cursor and the text inside, as `a(` takes
parentheses. `a}` is the same.

```keys
"x = {\n  |a\n}\ny" "da{" "x =| \ny"
```

## i"

The text inside the double quotes around the cursor, without them. It
finds the string as `a"` does. `2i"` takes the quotes too, without the
blanks around them. On an empty string it selects nothing, and `ci"`
still starts insert mode between the quotes.

```keys
"x = \"a|b\" y" "di\"" "x = \"|\" y"
"x = \"a|b\" y" "d2i\"" "x = | y"
"|x \"a\"" "ci\"b<Esc>" "x \"|b\""
"x \"|\"" "ci\"b<Esc>" "x \"|b\""
```

## i'

The text inside single quotes, as `i"` takes the text inside double
quotes.

```keys
"x 'a|b'" "di'" "x '|'"
```

## i(

The text inside the parentheses around the cursor, or the n-th pair out,
found as `a(` finds them. When the `(` ends its line and the `)` starts
its own, it takes the whole lines between them, linewise. `i)` is the
same.

```keys
"f(a, |b)" "di(" "f(|)"
"f(a, (|b))" "d2i(" "f(|)"
"f(\n  a,\n  |b\n)" "di(" "f(\n|)"
```

## i<

The text inside the angle brackets, as `i(` takes the text inside
parentheses. `i>` is the same.

```keys
"Vec<|int>" "ci<lt>u8<Esc>" "Vec<u|8>"
```

## i[

The text inside the square brackets, as `i(` takes the text inside
parentheses. `i]` is the same.

```keys
"x[|i + 1]" "di[" "x[|]"
```

## i`

The text inside backticks, as `i"` takes the text inside double quotes.

```keys
"run `l|s -a`" "di`" "run `|`"
```

## iB

The same as `i{`.

## ib

The same as `i(`.

## il

The cursor's line without the blanks at its start and end. Unlike `_`,
it selects characters, so `yil` copies no line break. A count changes
nothing.

```keys
"  foo |bar  " "dil" "  |  "
"  foo |bar  \nx" "yilGp" "  foo bar  \nxfoo ba|r"
```

## ip

The paragraph, n times, where each run of blank lines between paragraphs
counts as one. A line of only blanks counts as blank. Linewise.

```keys
"|a\nb\n\nc" "dip" "|\nc"
"|a\nb\n\nc\n\nd" "d3ip" "|\nd"
```

## is

The sentence, n times, where the blanks between two sentences count as
one. It stays in the paragraph, and fails when the paragraph has fewer
left. When the text ends a line and starts at or before its first
non-blank, it takes whole lines.

```keys
"One. T|wo. Three." "dis" "One. | Three."
"One. T|wo. Three." "d2is" "One. |Three."
```

## it

The text inside the n-th pair of tags around the cursor, between the `>`
of the opening tag and the `<` of the closing one, found as `at` finds
the pair.

```keys
"<p>a <b>|x</b></p>" "dit" "<p>a <b>|</b></p>"
"<p>a <b>|x</b></p>" "d2it" "<p>|</p>"
```

## iW

n runs of non-blanks or of blanks, as `iw` takes words.

```keys
"foo |a.b bar" "diW" "foo | bar"
```

## iw

n runs of word characters, other non-blanks, or blanks, as `w` counts
words, each run one. It stays on the cursor's line, and fails on an empty
line or when the line has fewer runs left.

```keys
"foo |bar baz" "diw" "foo | baz"
"foo |bar baz" "d3iw" "foo| "
"foo|  bar" "diw" "foo|bar"
```

## i{

The text inside the braces, as `i(` takes the text inside parentheses.
When the `{` ends its line and the `}` starts its own, as around a block
of code, it takes the lines between them, so `ci{` changes the body and
keeps the first line's indent. `i}` is the same.

```keys
"if x {\n\t|a\n\tb\n}" "di{" "if x {\n|}"
"if x {\n\t|a\n\tb\n}" "ci{c<Esc>" "if x {\n\t|c\n}"
```

## [n

The syntax node before the one at the cursor or the selection, among the
nodes of the same parent. n goes back n nodes. It fails at the parent's
first node.

## ]n

The syntax node after the one at the cursor or the selection, among the
nodes of the same parent, as `[n` takes the one before.

## aa

An argument or parameter and the comma and blanks after it, up to the
next one. The last one takes the comma before it instead, so `daa`
leaves a list that still reads right.

## ac

The class at the cursor, the smallest that holds it or the selection, as
the grammar's query names one. Blanks at its end stay out.

## af

The function at the cursor, the smallest that holds it or the selection,
as the grammar's query names one. Blanks at its end stay out. A count
selects the function around it, so `2af` in a function literal selects
the function that holds it.

## gc

The comment block at the cursor, which is the lines around it commented in
the language's comment syntax, up to a line that is not commented or is
blank. It selects whole lines, so `dgc` deletes the block, `cgc` changes
it, `ygc` yanks it and `gcgc` uncomments it. It works after an operator
only, since `gc` in visual mode comments the selection. On a line that is
not commented it fails. It comes with the comment module.

## an

The smallest syntax node that holds the character under the cursor, or
in visual mode the selection. When the selection is already that node, it takes
the node's parent, so each `an` climbs one level, and a count climbs n.

## ia

An argument or parameter without its comma.

## ic

The body of the class at the cursor, as `ac` takes the whole class.

## if

The body of the function at the cursor, as `af` takes the whole function.

## in

The node `an` last climbed from, so `in` after `an` goes back down one
level. Without that, it takes the first child of the selected node. When
the selection is not a node, it takes the first node inside it, or else
the smallest node that holds it.
