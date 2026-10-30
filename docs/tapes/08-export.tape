# o-json guide: export from the command line
# Regenerate (from the repo root): docs/tapes/record.sh 08-export

Output "docs/img/08-export.gif"

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1600
Set Height 900
Set Padding 12
Set Framerate 10
Set TypingSpeed 60ms
Set CursorBlink false

# Off camera: throwaway HOME and XDG dirs, so no owner config or state is read or written; truecolor for the 24-bit colours.
Hide
Type "export HOME=$(mktemp -d /tmp/o-json-guide/oj.XXXX) && export XDG_CONFIG_HOME=$HOME/c XDG_STATE_HOME=$HOME/s XDG_DATA_HOME=$HOME/d XDG_CACHE_HOME=$HOME/k COLORTERM=truecolor PS1='$ ' && cd /tmp/o-json-guide/vhs-work && clear && alias o-json=/tmp/o-json-guide/bin/o-json"
Enter
Sleep 2s
Show
Sleep 1s

Type "o-json export sample.json --of .items"
Sleep 400ms
Enter
Sleep 2s
Type "o-json export sample.json --of .items --format md --columns id,name,owner.name"
Sleep 400ms
Enter
Sleep 2.5s
Type "o-json get sample.json '.items[].owner.name' --raw"
Sleep 400ms
Enter
Sleep 2.5s
