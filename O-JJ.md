# o-jj trial

Build with `GOWORK=off go build -o /tmp/o-jj/bin/o-jj .`. The executable
uses `o-jj` for its CLI name and the ochazuke config and state directories:
`$XDG_CONFIG_HOME/o-jj` and `$XDG_STATE_HOME/o-jj` (or the matching paths
under `~`). It does not use the installed `o-json` directories.

o-jj builds Design D (`~/hub/o-plans/plans/o-json-modes.md`) close to the
owner-approved prototype (`/tmp/o-jj/prototype.html`).

## Modes

Each pane, and each view of the data pane, declares a mode; a key lifts
you into a doing-mode; `esc` drops one layer. The badge names the mode in
its trial colour (o-jj colours the application accent by mode).

| Mode | Badge | Declared by / entered by | Keys of its own |
|---|---|---|---|
| SHAPE | `SHAPE` | the structure pane | the structure keys; `ctrl+w o` keeps the structure pane |
| TABLE | `TABLE` | the data pane's table, object row or fields | `i` → EDIT, `r` → EDIT and the value editor; `ctrl+w o` keeps the data pane |
| NESTED | `NESTED` | the cursor in a hover sub-table | as TABLE |
| FLAT | `FLAT` | `D`/`tab` to the Flat view | `y y` line, `y p` path, `y v` value, `enter` moves the structure cursor to the path |
| JSON | `JSON` | `D`/`tab` to the JSON view | `z a` `z o` `z c` fold the value under the line, `z M` fold all, `z R` open all, `%` matching bracket, `y y` value, `y p` path, `enter` jump |
| SEARCH | `SEARCH · SUBSTR` / `· FUZZY` / `· REGEX` on the line, `SEARCH` on the Results | `/` | on the line: `tab`/`shift+tab` type, `ctrl+n`/`ctrl+p` hits, `enter` shows the Results, `esc` cancels; on the Results: `n`/`N`, `enter` jumps the structure (the pane stays), `y y` value, `y p` path, `esc` clears |
| JQ | `INSERT` on the line, `JQ` on the result | `/` then `shift+tab` (jq is last in the cycle), `m` on the structure | on the line: `ctrl+n`/`ctrl+p` slots, `ctrl+f` function; the result is a table view; `esc` on the pane clears it |
| EDIT | `EDIT` | `i`, `r` on the data pane | `r`/`i` edit, `x`/`d d` delete, `o`/`O` add, `K` rename, `p` put, `esc` leaves; leaving the pane leaves EDIT |
| VISUAL | `VISUAL LINE` | `v`, `V` in either pane | ochazuke's |
| RAW | (the editor has the terminal) | `g r` | the editor's own |
| SPLIT | `SPLIT` | `ctrl+w v` | `d p` push the shape (or the visual selection) to the restructured side, `d o` pull the original's shape; `v j j {` / `[` wrap; `ctrl+w o` keeps the focused side |

- `D` or `tab` cycles the data views from either pane (`shift+tab`
  back, counts skip): table, fields, flat, JSON, then the Results and the
  jq result while a search or query is live. The structure path's own
  view is remembered per path; Results and jq result are overlays shown
  over every path until `esc` clears them or the cycle steps off them.
- `ctrl+w h` / `ctrl+w l` move to the left / right pane and stop at the
  edge; `ctrl+w w` and `ctrl+a o` cycle. `ctrl+w o` shows only one pane:
  which one is each mode's binding row (`viewer/shape`: structure;
  `viewer/table`, `nested`, `flat`, `json`: data; elsewhere the focused
  pane, on the split the focused side). `ctrl+w o` again, `ctrl+w =`, or
  moving the focus to the hidden pane brings both back.
- `g m` (`:viewer mode`) opens the fuzzy mode picker; every row enters
  its mode (JQ opens the search line in its jq type, as the prototype
  does; RAW starts the editor).
- RAW: `g r` writes the edited document (from the data pane: the row's
  value) as pretty JSON to a temp file and runs `raw.editor`, else
  `$VISUAL`, `$EDITOR`, `nvim`, in the foreground (`ochazuke.Foreground`,
  Bubble Tea's ExecProcess). Unchanged: `raw: no change`. Valid: one set
  patch at the value's path, so `u` undoes the whole session. Invalid:
  `raw: invalid JSON at line L, column C` and the editor reopens at
  `+L` with the text as left; a third invalid exit drops the edits.

## Settings (defaults)

- `modes.declared` `on`: off keeps the plain NORMAL badge (view keys still work).
- `modes.edit-gate` `on`: off lets the value keys answer in TABLE as in o-json.
- `search.type` `substring`: the type `/` opens in (`substring`, `fuzzy`, `regex`, `jq`).
- `raw.editor` empty: the editor command line for `g r`.

## Compared with Vim

| Vim form | Help tag | o-jj |
|---|---|---|
| `v`, `V` | `:help visual-mode` | Similar: select structure or data rows. |
| `2D` | `:help D` | Different: skip two data views, not delete to line end. |
| `tab` | `:help CTRL-I` | Different: cycle data views, not jump to an older position. |
| `ctrl+w h`, `ctrl+w l` | `:help CTRL-W_h` | Same: the window on that side; nothing past the edge. |
| `ctrl+w w` | `:help CTRL-W_w` | Same: the next pane, wrapping. |
| `ctrl+w o` | `:help CTRL-W_o` | Similar: other panes hidden, not closed; `ctrl+w o` again or `ctrl+w =` restores (Vim has no undo of `:only`). |
| `ctrl+w =` | `:help CTRL-W_=` | Similar: restores a `ctrl+w o`, else resets the divider. |
| `ctrl+w v` | `:help CTRL-W_v` | Similar: opens the original and edited structure split on a separate page. |
| `dp`, `do` | `:help dp`, `:help do` | Similar: move a shape across the split; only original → restructured. |
| `za`, `zo`, `zc`, `zM`, `zR` | `:help fold-commands` | Same on the JSON view's values. |
| `%` | `:help %` | Same: the matching bracket line. |
| `yy` | `:help yy` | Similar: the line (FLAT) or the value at the line (JSON). |
| `/`, `n`, `N` | `:help /` | Similar: keys and values, with `tab` match types; hits listed in the Results view. |
| `i`, `esc` | `:help i` | Similar: `i` enters EDIT on the data pane; the value keys need it. |
| `r` | `:help r` | Different: edits the whole value in the value editor. |
| `x` | `:help x` | Different: deletes the value under the cursor. |
| `g m` | n/a | o-jj's mode chooser; fuzzy by name. |
| `g r` | n/a (`:help gr` is virtual replace) | The document in `$EDITOR`, as `:!` would. |

## Rough or missing

- While typing in the jq type of the line the badge reads `INSERT`:
  ochazuke's completion list opens only in a mode named `insert`, so the
  jq line keeps it (path completion would break otherwise). The search
  types use `search_·_substr`, `search_·_fuzzy` and `search_·_regex`
  modes that extend insert, which is how their badges read `SEARCH ·
  SUBSTR` (upper case; the tag is not lower case as in the prototype).
  Their input undo groups per change, not per insert session.
- The hits are computed on the event loop, capped at 5000; a very large
  document will feel it. The jq type runs off the loop as before.
- `esc` on the line cancels a search (the text goes) but keeps a jq query
  (as the query line always did). With a search cycled off its Results
  view, `esc` on the structure still clears it (`search.clear` comes before
  `viewer.clear`).
- `enter` on a FLAT or JSON line moves the structure cursor; the data
  pane then follows the structure (o-json's invariant), so it changes to
  that path's view. The prototype kept the pane on the old view. On the
  Results view the pane stays (the overlay), as in the prototype.
- RAW replaces the value with one set patch, not a diff into many patches;
  a key reorder in the editor is kept (the patch is the text's order).
  `g r` in VISUAL (`R` on a selection) and a tuios pane for the editor
  (`raw.pane`) are not built; RAW is always foreground.
- The footer often truncates before the per-mode `ctrl+w o` hint; `?`
  shows it.
- The value editor's `esc esc` returns to EDIT; the detail's value edits
  are not gated by EDIT.
- The test suite runs with `modes.edit-gate` off by default (it was
  written for o-json's ungated edits, `editGateDefault` in TestMain); the
  EDIT tests and stories turn it on.
- Needs ochazuke later: an instance mode pin (the declared mode is set by
  o-jj after each event and in render), declarable `mode.<name>` badge
  tones (o-jj recolours the whole accent), insert-like modes that
  completion and the input's undo grouping accept, a lower-case badge tag,
  and a pane "only" in the layout (o-jj draws it itself).
- `:mode` cannot be a bare alias while ochazuke owns the `mode` noun;
  `:viewer mode` opens the chooser.
