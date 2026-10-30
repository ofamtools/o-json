# o-json guide: the restructure page: send, move out, rename, write
# Regenerate (from the repo root): docs/tapes/record.sh 06-restructure

Output "docs/img/06-restructure.gif"

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

# M opens the restructure page, empty; s sends shapes across from the original.
Type "jjj"
Sleep 500ms
Type "M"
Sleep 1.8s
Type "jj"
Sleep 500ms
Type "s"
Sleep 1.4s
Type "jjj"
Sleep 500ms
Type "s"
Sleep 1.6s
# tab to the restructured tree; < < moves owner.name out of owner.
Tab
Sleep 800ms
Type "G"
Sleep 600ms
Type "<<"
Sleep 1.6s
# r renames it; the mapping updates live.
Type "r"
Sleep 700ms
Type "owner_name"
Sleep 500ms
Escape
Sleep 1.2s
# Down to the empty owner wrapper: d d deletes it.
Type "j"
Sleep 400ms
Type "dd"
Sleep 1.6s
# w writes the new document.
Type "w"
Sleep 1s
Type "people.json"
Sleep 500ms
Enter
Sleep 2.5s

Hide
Type "Q"
