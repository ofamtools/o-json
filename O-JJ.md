# o-jj trial

Build with `GOWORK=off go build -o /tmp/o-jj/bin/o-jj .`. The executable
uses `o-jj` for its CLI name and the ochazuke config and state directories:
`$XDG_CONFIG_HOME/o-jj` and `$XDG_STATE_HOME/o-jj` (or the matching paths
under `~`). It does not use the installed `o-json` directories.

## Modes and keys in this spike

- **SHAPE** is the viewer's structure pane. **TABLE** is the data table or
  object fields form. **NESTED** is the cursor inside a hover subtable.
  **FLAT** shows leaf `path = value` lines. **JSON** shows indented JSON.
  **JQ** names `m` on the structure and the data pane while it shows live jq
  results. **SPLIT** names
  either structure on the restructure page. The badge follows focus and view.
  Each declared mode has a distinct colour; o-jj uses ochazuke's application
  accent, which also colours the focused border and title.
- `D` or `tab` cycles applicable data views from either pane;
  `shift+tab` goes backward. Counts skip views (`2D`). The selected view is
  remembered per structure path. `v` and `V` start visual selection in
  either pane. `viewer.raw` remains available from the command palette.
- `ctrl+a o` cycles viewer panes or restructure sides. `ctrl+w h`, `l`, and
  `w` move pane focus; `ctrl+w v` opens the existing restructure split.
- `g m` opens the fuzzy mode picker. Each row shows the mode, its trial
  colour, purpose, and entry key. Choosing SHAPE, TABLE, FLAT, JSON, JQ,
  or SPLIT moves to that mode. The picker lists the later modes too and
  explains when their entry action is not yet available.
- `modes.declared=off` keeps the normal badge. The view keys still work.

## Compared with Vim

| Vim form | Help tag | o-jj |
|---|---|---|
| `v`, `V` | `:help visual-mode` | Similar: select structure or data rows; data selection currently covers the visible window. |
| `2D` | `:help D` | Different: skip two data views, not delete to line end. |
| `tab` | `:help CTRL-I` | Different: cycle data views, not jump to an older position. |
| `ctrl+w h`, `ctrl+w l`, `ctrl+w w` | `:help windows` | Similar: move focus between o-jj's two panes. |
| `ctrl+w v` | `:help CTRL-W_v` | Similar: opens the existing original and edited structure split on a separate page. |
| `g m` | n/a | o-jj's mode chooser; fuzzy by name. |

## Rough or missing

This is an early slice of Design D. FLAT and JSON are read-only lines; their
line yank, jump and fold keys are not implemented. SEARCH, NESTED, EDIT,
VISUAL, and RAW are listed but do not yet have picker entry actions. `:mode`
cannot be a bare alias while ochazuke owns the `mode` noun; `:viewer mode`
opens the chooser. Search results, EDIT gating, `ctrl+w o`, split push/pull,
and RAW/Neovim integration
are not implemented. JQ results use the current table view and query line;
the unified search line is not implemented.
`ctrl+w h` and `l` currently use ochazuke's wrapping previous/next focus
walk, so they can wrap at an edge instead of staying in the left/right pane.
The badge may update one frame after a focus move because ochazuke has noun
pins but no instance pin. Data visual selection is limited by the windowed
row list. A library instance pin and declarable `mode.<name>` badge tones are
needed in ochazuke later; the accent workaround colours more chrome than the badge.
