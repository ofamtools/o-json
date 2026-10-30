# o-jj trial

## DONE checklist (Design D and the prototype)

Status: **done**, **partial** (works with a gap named), **missing**.
Updated as each item lands; the sections below describe what is built.

### Modes and the badge

| Item | Keys | Status |
|---|---|---|
| SHAPE declared by the structure pane | `ctrl+w h` | done |
| TABLE declared by the table / fields view | `ctrl+w l`, `ctrl+a o` | done |
| NESTED while a hover sub-table is open under the cursor | `j` into it, `k` out, `esc` out (TABLE), `backspace`/`-` one level, `D` shows it | done |
| FLAT, JSON declared by their views | `D`, `tab`, `shift+tab`, `2 D` | done |
| SEARCH on the line (`SEARCH · SUBSTR`), SEARCH on the Results | `/`, `tab`, `enter` | done |
| Search badge tag lower case (`SEARCH · substr`) | | missing (ochazuke upper-cases the badge) |
| JQ: `m` on the structure, the jq result view declares JQ | `m`, `/` `shift+tab` | done |
| JQ badge while typing the jq line (prototype `JQ`) | `/` `shift+tab` | done (`JQ · LINE`: the mode `jq` is the result's; INSERT while a function slot is selected) |
| EDIT entered by `i`, `r` (+ value editor); `esc` / leaving the pane leaves | `i`, `r`, `esc` | done |
| VISUAL in both panes | `v`, `V`; data pane `j k g g G ctrl+d ctrl+u` extend | done |
| SPLIT badge on the restructure page | `ctrl+w v` | done |
| RAW | `g r` | done (foreground, or a tuios pane; no RAW badge while the editor has the terminal) |
| Mode picker | `g m` | done |
| Per-mode accent colours | | partial (o-jj recolours the whole accent; ochazuke tones later) |
| Opt-outs `modes.declared`, `modes.edit-gate` | | done |

### Footer and help

| Item | Keys | Status |
|---|---|---|
| Per-mode keys first on the footer (`ctrl+w o` …), other hints short | | done (the rows sit on the focused noun's mode table, which leads the footer) |

### Views and panes

| Item | Keys | Status |
|---|---|---|
| View cycle from either pane, remembered per path, counts | `D`, `tab`, `shift+tab` | done |
| Window keys: left/right stop at the edge, cycle | `ctrl+w h/l/w`, `ctrl+a o` | done |
| Only the mode's pane, restore | `ctrl+w o`, `ctrl+w =` | done |
| FLAT: yank line / path / value, jump | `y y`, `y p`, `y v`, `enter` | done |
| JSON: folds, bracket, yank, jump | `z a z o z c z M z R`, `%`, `y y`, `y p`, `enter` | done |

### SEARCH and JQ

| Item | Keys | Status |
|---|---|---|
| One line, type cycle substring / fuzzy / regex / jq | `/`, `tab`, `shift+tab` | done |
| Hits while typing, Results view, n/N, jump, yank | `ctrl+n/p`, `enter`, `n`, `N`, `y y`, `y p` | done |
| `esc` on the line cancels; `esc` on a pane clears | `esc` | done |
| `:sort`, `:sort!`, `:find` aliases | `:sort` | done (`:raw` would shadow `viewer.raw`: `g r`) |
| Hits computed off the event loop | | done (documents over 20000 nodes; the breadcrumb says `searching…`) |
| Undo groups per search typing session | `u` on the line | done (the line modes extend insert, ochazuke v0.61.0; a `tab` to another type starts a new group) |
| jq slots on `ctrl+n`/`ctrl+p` | | done |

### EDIT

| Item | Keys | Status |
|---|---|---|
| Value keys need EDIT | `x`, `d d`, `o`, `O`, `K`, `p` | done |
| Structure edits under EDIT (question 22) | `i` on the edited structure; `> < S t c d o O a p P F g p g k g v g e` need it | done (the original tree stays read-only; the SPLIT page keeps its keys live) |

### RAW

| Item | Keys | Status |
|---|---|---|
| Document / row value in the editor, foreground | `g r` | done |
| Invalid → reopen at line, third invalid drops | | done |
| Edit back as path-addressed patches (a diff), one undo step | `u` | done (a reordered object is one set: no reorder patch) |
| VISUAL `R` opens the selection in RAW | `V j R` | done (data pane rows; the structure's selection is refused) |
| tuios pane, `ctrl+a d` detach, `g r` re-attach (`raw.pane auto/foreground`) | `g r`, `ctrl+a d` | done (tested with a fake tuios; not walked in a live session) |

### SPLIT

| Item | Keys | Status |
|---|---|---|
| Original beside edited | `ctrl+w v` | done |
| Push / pull | `d p`, `d o` | done |
| Wrap the selection | `v j j j {`, `[`, `}`, `]` | done |

### ochazuke v0.61.0 (the library features replace o-jj's workarounds)

| Item | Status |
|---|---|
| Bump to v0.61.0 | done |
| Instance pin: data rows pinned per view, structure pinned to `shape` (no per-event mode setting) | missing |
| `esc` rows: `mode.set {mode: declared}` | missing |
| Badge colours: `SetModeTone` per mode (no whole-accent recolour) | missing |
| SEARCH / JQ line modes extend insert (completion, per-session undo, `JQ` badge while typing) | done |
| `ctrl+w o` / `ctrl+w =`: `ochazuke.pane.only` / `pane.restore`, `SplitFocusBlock` | missing |

### Grammar inside SHAPE / TABLE / NESTED (Design B)

| Item | Keys | Status |
|---|---|---|
| Operators wait for a motion or object | `y`, `d`, `c`, `s`, `!` (structure: `y d c !`; original tree `y !`) | done |
| JSON text objects | `i`/`a` + `v k p o a r c t` (data), `v k p s` (structure) | done |
| Counts, `.` repeat, registers | `2 d j`, `.`, `" a y i r` | done |
| Shortcut rows | `y y`, `y p`, `y k`, `Y`, `d d`, `c c`, `s s`, `! !`, `x` | done |


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
| NESTED | `NESTED` | the cursor in a hover sub-table | as TABLE; `esc` leaves the sub-tables, `backspace`/`-` one level, `D` shows the value as the pane's view |
| FLAT | `FLAT` | `D`/`tab` to the Flat view | `y y` line, `y p` path, `y v` value, `enter` moves the structure cursor to the path |
| JSON | `JSON` | `D`/`tab` to the JSON view | `z a` `z o` `z c` fold the value under the line, `z M` fold all, `z R` open all, `%` matching bracket, `y y` value, `y p` path, `enter` jump |
| SEARCH | `SEARCH · SUBSTR` / `· FUZZY` / `· REGEX` on the line, `SEARCH` on the Results | `/` | on the line: `tab`/`shift+tab` type, `ctrl+n`/`ctrl+p` hits, `enter` shows the Results, `esc` cancels; on the Results: `n`/`N`, `enter` jumps the structure (the pane stays), `y y` value, `y p` path, `esc` clears |
| JQ | `JQ · LINE` on the line, `JQ` on the result | `/` then `shift+tab` (jq is last in the cycle), `m` on the structure | on the line: `ctrl+n`/`ctrl+p` slots, `ctrl+f` function; the result is a table view; `esc` on the pane clears it |
| EDIT | `EDIT` | `i`, `r` on the data pane; `i` on the edited structure | data: `r`/`i` edit, `x`/`d d` delete, `o`/`O` add, `K` rename, `p` put; structure: its edit keys; `esc` leaves; leaving the pane leaves EDIT |
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
  which one is each mode's binding row on the focused noun (`node/shape`,
  `shape/shape`: structure; `item/table`, `field/table`, `item/nested`,
  `field/nested`, `line/flat`, `line/json`, `line/search`: data; elsewhere the focused
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

## The grammar (SHAPE, TABLE, NESTED, EDIT)

`y` `d` `c` `s` `!` enter ochazuke's `operator_pending` mode (the
editor's rows, `bindings/editor.json`); the next key is a motion (`j`,
`k`: this row and the next, Vim's `dj`; counts multiply, `2 d 3 j`) or
`i` / `a` (the modes `operator_pending_i` / `operator_pending_a`, badge
`OPERATOR PENDING I`) and a JSON text object:

| Object | `i` (inner) | `a` (around) |
|---|---|---|
| `v` value | the cell's value | `"key": value` |
| `k` key | the key's name | the field (`d a k` = `d d`) |
| `p` path | `.items[3].name` | `.items[].name` |
| `o` object | the enclosing object | with its key |
| `a` array | the enclosing array | with its key |
| `r` row | the record | the same |
| `c` column | the column's values (an array) | `{"name": [...]}` |
| `t` table | the table's array | the same |
| `s` subtree | (structure) the node's subtree | the same |

| Operator | Does | Needs EDIT (`modes.edit-gate`) |
|---|---|---|
| `y` | copies (`" a` picks the register) | no |
| `d` | value patches, one undo step | yes |
| `c` | the value editor (`c i k`: rename the key) | yes |
| `s` | `s i c` sorts the table by the column (a view; `s s` again reverses, `s` on `#` clears); `s i o`, `s i a` sort the value (a patch) | the patch only |
| `!` | opens the `/` line in its jq type with the path typed (`! i c` → `.items[].name`; `a` the structure path) | no |

Shortcut rows on the same verbs: `y y` value, `y p` path, `y k` key,
`Y` the row, `d d` the field (an item: the item), `c c`, `s s` the
column, `! !`; `x` stays `value.delete`. On the structure `y y` and
`d d` keep `node.yank`/`node.delete`, `c c` renames, `y p`/`y k`/`y i v`
copy the path, key and the values at the path. One verb per noun on one
handler (`item.operate`, `field.operate`, `node.operate`,
`shape.operate`, operate.go); which pairs exist is `operatorPairs`. `.`
repeats the last operate at the new focus. With `modes.declared off` the
operators are not bound (they live on the mode tables) and the old keys
answer.

## Settings (defaults)

- `modes.declared` `on`: off keeps the plain NORMAL badge (view keys still work).
- `modes.edit-gate` `on`: off lets the value keys answer in TABLE and the structure's edit keys in SHAPE, as in o-json.
- `search.type` `substring`: the type `/` opens in (`substring`, `fuzzy`, `regex`, `jq`).
- `raw.editor` empty: the editor command line for `g r`.
- `raw.pane` `auto`: a tuios window inside tuios, else the foreground; `foreground` always suspends.
- `raw.detach` (an nvim/vim `-c` mapping): `ctrl+a d` in the editor's pane focuses o-jj; empty passes nothing.
- `raw.pane-grants` `read,write`: the editor window's tuios grants.

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
| `d{motion}`, `y{motion}`, `c{motion}` | `:help operator` | Similar: the motions are rows (`j`, `k`). |
| `iw`, `aw`, `i"` | `:help text-objects` | Similar: JSON's things (`iv ak ip ao ic ...`). |
| `yy`, `dd`, `Y`, `x` | `:help yy` | Similar: value, field, row, value. |
| `.` | `:help .` | Same for operators. |
| `"a` | `:help quote` | Same. |
| `!{motion}` | `:help !` | Similar: jq only, into the `/` line. |
| `:sort` | `:help :sort` | Different: `s i c` sorts the table's view, not the text. |

## Rough or missing

- The search line's modes (`search_·_substr`, `_fuzzy`, `_regex`,
  `jq_·_line`) extend insert (ochazuke v0.61.0), so the path completion,
  the input's undo groups and the badge (`SEARCH · SUBSTR`, `JQ · LINE`)
  all work; the badge is upper case (ochazuke upper-cases it; the
  prototype's tag is lower case). While a jq function's slot is selected
  to type over, the line is in plain insert: ochazuke's
  `Input.SelectText` sets the mode named insert (a library bug, worked
  around in views.go).
- The hits (capped at 5000) are computed on the event loop for a
  document of up to 20000 nodes (`searchLoopNodes`), off it for a larger
  one: the last hits stay, the breadcrumb says `searching…`, and an
  answer for an older text is dropped. The jq type runs off the loop as
  before.
- `esc` on the line cancels a search (the text goes) but keeps a jq query
  (as the query line always did). With a search cycled off its Results
  view, `esc` on the structure still clears it (`search.clear` comes before
  `viewer.clear`).
- `enter` on a FLAT or JSON line moves the structure cursor; the data
  pane then follows the structure (o-json's invariant), so it changes to
  that path's view. The prototype kept the pane on the old view. On the
  Results view the pane stays (the overlay), as in the prototype.
- RAW maps the edit back as path-addressed patches (rawdiff.go: a set
  per changed scalar, a delete or an add beside its neighbour per key,
  an array's head and tail kept and the middle compared pairwise), all
  one undo step; an object whose kept keys changed order is one set at
  its path (there is no reorder patch). `R` in the data pane's VISUAL
  sends the selected rows as one array, mapped back item by item (a
  different item count applies nothing). Inside tuios (`raw.pane auto`)
  the editor runs in a new tuios window (`tuios new-window … --print-id
  -- nvim -c <raw.detach> +L file`, grants `raw.pane-grants`); o-jj keeps
  running, the breadcrumb says `RAW running`, `g r` focuses the window,
  the editor's `ctrl+a d` focuses o-jj's pane, and the window's end
  reads the file back. Not walked in a live tuios session yet; whether
  `write` is the grant `focus-window` needs is unchecked.
- The footer leads with the mode's own keys: they are rows of the focused
  noun's mode table, and ochazuke lists the innermost level first. The
  search line's keys read type, hits, show, leave; `ctrl+f` (functions)
  answers only in the jq type.
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
