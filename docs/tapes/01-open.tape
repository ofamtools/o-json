# o-json guide: the empty document, then e opens a file
# Regenerate (from the repo root): docs/tapes/record.sh 01-open

Output "docs/img/01-open.gif"

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
Type "export HOME=$(mktemp -d /tmp/o-json-guide/oj.XXXX) && export XDG_CONFIG_HOME=$HOME/c XDG_STATE_HOME=$HOME/s XDG_DATA_HOME=$HOME/d XDG_CACHE_HOME=$HOME/k PS1='$ ' && cd /tmp/o-json-guide/vhs-work && clear && /tmp/o-json-guide/bin/o-json"
Enter
Sleep 2s
Show
Sleep 1s

# The empty document names the keys that fill it. e opens the path chooser.
Sleep 1.5s
Type "e"
Sleep 1.5s
Type "sample"
Sleep 800ms
Enter
Sleep 2.5s

Hide
Type "Q"
