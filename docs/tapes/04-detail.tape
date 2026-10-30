# o-json guide: drill into a row in the detail
# Regenerate (from the repo root): docs/tapes/record.sh 04-detail

Output "docs/img/04-detail.gif"

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

# Select .items, Tab into the table, enter opens row 0 in the detail.
Type "jjj"
Sleep 600ms
Tab
Sleep 800ms
Enter
Sleep 1.5s
# l steps into owner, h steps back out, q closes.
Type "jj"
Sleep 600ms
Type "l"
Sleep 1.5s
Type "h"
Sleep 1.2s
Type "j"
Sleep 500ms
Type "l"
Sleep 1.5s
Type "h"
Sleep 800ms
Type "q"
Sleep 1.5s

Hide
Type "Q"
