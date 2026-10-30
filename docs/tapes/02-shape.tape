# o-json guide: the structure tree, original vs edited, the type filter
# Regenerate (from the repo root): docs/tapes/record.sh 02-shape

Output "docs/img/02-shape.gif"

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

# Walk the edited structure (each node's from column), then T: the original, with counts and ? on optional fields.
Type "jjjj"
Sleep 1s
Type "T"
Sleep 2.5s
# The type filter: strings, then numbers; esc clears it.
Type "t"
Sleep 1.8s
Type "t"
Sleep 1.8s
Escape
Sleep 1s
Type "T"
Sleep 1.5s

Hide
Type "Q"
