# o-json guide: the jq query line, and query mode on the tree
# Regenerate (from the repo root): docs/tapes/record.sh 07-query

Output "docs/img/07-query.gif"

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
Sleep 1s

# / focuses the query line; results show as you type.
Type "/"
Sleep 800ms
Type ".items[] | select(.tags | length > 0) | .owner"
Sleep 2s
Escape
Sleep 1s
# Clear it: / then ctrl+u, esc.
Type "/"
Sleep 400ms
Ctrl+U
Sleep 800ms
Escape
Sleep 800ms
# m: query mode. [ opens an array, . writes the highlighted field, ] closes it.
Type "m"
Sleep 1s
Type "j"
Sleep 400ms
Type "["
Sleep 800ms
Type "."
Sleep 900ms
Type "j"
Sleep 400ms
Type "."
Sleep 900ms
Type "]"
Sleep 2s
Type "m"
Sleep 1.2s

Hide
Type "Q"
