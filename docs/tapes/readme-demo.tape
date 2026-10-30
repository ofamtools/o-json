# o-json README demo: the structure, the table, a hover sub-table, a value edit, a rename, the live jq panel
# Regenerate (from the repo root): docs/tapes/record.sh readme-demo

Output "assets/demo.gif"

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1600
Set Height 900
Set Padding 12
Set Framerate 10
Set TypingSpeed 60ms
Set CursorBlink false

# Off camera: throwaway HOME and XDG dirs, so no owner config or state is read or written; truecolor, the dark theme and rich chrome colour pinned; keys.echo shows the keys.
Hide
Type "export HOME=$(mktemp -d /tmp/o-json-guide/oj.XXXX) && export XDG_CONFIG_HOME=$HOME/c XDG_STATE_HOME=$HOME/s XDG_DATA_HOME=$HOME/d XDG_CACHE_HOME=$HOME/k COLORTERM=truecolor PS1='$ ' && mkdir -p $XDG_CONFIG_HOME/o-json && cd /tmp/o-json-guide/vhs-work"
Enter
Type `printf '{"keys.echo": "footer", "keys.echo-linger": "4s", "theme": "dark", "chrome.color": "rich"}\n' > $XDG_CONFIG_HOME/o-json/settings.json && clear && /tmp/o-json-guide/bin/o-json sample.json`
Enter
Sleep 2s
Show
Sleep 1.2s

# The structure: one row per path. Down to .items, an array of objects: the data panel is its table.
Type "jjj"
Sleep 1.5s

# Into the table, across to owner: its sub-table opens under the row; j walks into it.
Tab
Sleep 700ms
Type "ll"
Sleep 1.2s
Type "l"
Sleep 1.5s
Type "j"
Sleep 1.4s

# Back up to the row's name: r opens the value editor.
Type "k"
Sleep 400ms
Type "h"
Sleep 600ms
Type "r"
Sleep 1s
Type "omega"
Sleep 600ms
Enter
Sleep 1.5s

# Back to the structure: r renames .name to title, one change to the shape.
Tab
Sleep 600ms
Type "kk"
Sleep 500ms
Type "r"
Sleep 700ms
Type "title"
Sleep 500ms
Escape
Sleep 1.5s
# g j: the live jq panel follows every edit; tab shows the mapping, the rename on its first line.
Type "gj"
Sleep 1.8s
Tab
Sleep 2.5s

Hide
Type "Q"
