# o-jj trial

Build with `GOWORK=off go build -o /tmp/o-jj/bin/o-jj .`. The executable
uses `o-jj` for its CLI name and the ochazuke config and state directories:
`$XDG_CONFIG_HOME/o-jj` and `$XDG_STATE_HOME/o-jj` (or the matching paths
under `~`). It does not use the installed `o-json` directories.

## Modes and keys in this spike

- **SHAPE** is the structure pane. **TABLE** is the data table or object
  fields form. **FLAT** shows leaf `path = value` lines. **JSON** shows
  indented JSON. The badge follows the focused pane and the selected view.
- `D` or `tab` cycles applicable data views from either pane;
  `shift+tab` goes backward. Counts skip views (`2D`). The selected view is
  remembered per structure path. `v` and `V` start visual selection in
  either pane. `viewer.raw` remains available from the command palette.
- `modes.declared=off` keeps the normal badge. The view keys still work.

## Compared with Vim

| Vim form | Help tag | o-jj |
|---|---|---|
| `v`, `V` | `:help visual-mode` | Similar: select structure or data rows; data selection currently covers the visible window. |
| `2D` | `:help D` | Different: skip two data views, not delete to line end. |
| `tab` | `:help CTRL-I` | Different: cycle data views, not jump to an older position. |

## Rough or missing

This is an early slice of Design D. FLAT and JSON are read-only lines; their
line yank, jump and fold keys are not implemented. Search results, jq
results, a mode picker, EDIT gating, split keys, RAW/Neovim integration and
distinct mode colours are not implemented. The badge may update one frame
after a focus move because ochazuke has noun pins but no instance pin. Data
visual selection is limited by the windowed row list. A library instance
pin and declarable `mode.<name>` tones are needed in ochazuke later.
