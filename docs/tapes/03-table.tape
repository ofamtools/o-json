# o-json guide: an array as a table; nested data opens under the row
# Regenerate (from the repo root): docs/tapes/record.sh 03-table

Output "docs/img/03-table.gif"

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
Type `printf '{"keys.echo": "footer", "keys.echo-linger": "4s", "theme": "dark", "chrome.color": "rich"}\n' > $XDG_CONFIG_HOME/o-json/settings.json && clear && /tmp/o-json-guide/bin/o-json walk.json`
Enter
Sleep 2s
Show
Sleep 1s

# The document is an array: Tab into its table, then walk right across the columns.
Tab
Sleep 1s
Type "lll"
Sleep 1.5s
Type "l"
Sleep 1.5s
Type "l"
Sleep 1.5s
# j walks into the sub-table under the row, and on out to the next row.
Type "j"
Sleep 1.2s
Type "l"
Sleep 1.5s
Type "j"
Sleep 1.2s
Type "j"
Sleep 1.5s

Hide
Type "Q"
