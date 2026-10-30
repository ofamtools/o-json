# o-json guide: edit values in the value editor, undo, write
# Regenerate (from the repo root): docs/tapes/record.sh 05-edit

Output "docs/img/05-edit.gif"

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1132
Set Height 606
Set Padding 12
Set Framerate 10
Set TypingSpeed 60ms
Set CursorBlink false

# Off camera: throwaway HOME and XDG dirs, so no owner config or state is read or written.
Hide
Type "export HOME=$(mktemp -d /tmp/o-json-guide/oj.XXXX) && export XDG_CONFIG_HOME=$HOME/c XDG_STATE_HOME=$HOME/s XDG_DATA_HOME=$HOME/d XDG_CACHE_HOME=$HOME/k PS1='$ ' && cd /tmp/o-json-guide/vhs-work && clear && /tmp/o-json-guide/bin/o-json sample.json"
Enter
Sleep 2s
Show
Sleep 1s

# Into the table; r on row 0's name opens the value editor.
Type "jjj"
Sleep 500ms
Tab
Sleep 600ms
Type "ll"
Sleep 400ms
Type "r"
Sleep 1.2s
Type "omega"
Sleep 600ms
Enter
Sleep 1.2s
# o adds a key to the row's object: the key, tab, the value.
Type "o"
Sleep 1.2s
Type "priority"
Sleep 400ms
Tab
Sleep 500ms
Type "1"
Sleep 600ms
Enter
Sleep 1.4s
# d d deletes a cell's field, u undoes it.
Type "j"
Sleep 400ms
Type "dd"
Sleep 1.4s
Type "u"
Sleep 1.4s
# w writes the edited document to a new file (the source is never touched).
Type "w"
Sleep 1.2s
Type "edited.json"
Sleep 500ms
Enter
Sleep 2.5s

Hide
Type "Q"
