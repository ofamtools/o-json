# o-json: agent guide

`o-json` (so `o json`) is the family's JSON structure and table viewer. It has a command line (`structure`, `table` and `get` print text, and the `doc` noun prints JSON for a host) and a two-panel ochazuke viewer. It never writes the document it reads unless asked: only `restructure --out`, `export --out`, the viewer's `viewer.write`, `viewer.export` and `restructure.write`/`export-map` (the restructure page, and `w` in the viewer's edited view) write a file (the recent list, `recent.json` in the state directory, aside), a new one unless overwriting is asked for (and confirmed, on the page); the source only with `--overwrite` or the page's `W`, which confirms (`restructure.overwrite-source`). Usage and keys are in README.md.

It is an ochazuke application, so ochazuke's rules apply here unchanged. Read `~/proj/ochazuke/AGENTS.md` first, especially "Building an application on ochazuke" and the Invariants: keys are binding rows (no key-name switches), every row is atomic, motions are visual, hints derive from bindings, Bubble Tea v2 idioms only, never bind F1 to F12. `~/proj/o-csv` is the sibling it copies its shape from (dispatch, shell, prompt, clipboard, key test helpers).

## Plans and todos

Family plans, todos and the roadmap live in `~/hub/o-plans/` (`README.md` is the index; this tool's plan is `plans/o-json.md`). Read it before planning work; add or update items there. Do not add a TODO.md or docs/plans/ to this repo.

## Where does X live

| Concern | File |
|---|---|
| The document model (from gmtm's jsonexplorer `tree.go`): `Node`, `Build`, `ParseError`, `Pretty`, `Compact`, key-order `MarshalJSON` | `doc.go` |
| jq-style paths: `Select` (fans out on `[]`), `Get` | `path.go` |
| Structure inference: `Shape`, `Infer`, `WriteStructure` | `shape.go` |
| Tables: `NewTable` (columns by frequency, optional last), text, markdown, JSON | `table.go` |
| Reading a file or stdin (`ReadDoc`, `splitFile`) | `input.go` |
| `main`, oden dispatch (a file word opens the viewer), the help note | `main.go` |
| The text commands `structure`, `table`, `get`, `restructure` | `commands.go` |
| Export: `viewer.open-in` (`openInApps`: o-csv in the foreground, a temp csv), the format table (`exportFormats`, extension → writer → jq step), `Export` (rows, dotted columns, `export.arrays`/`join`/`null`), the jq `@csv`/`@tsv` program, `viewer.export`, the extension a write infers (`encodeFor`), `o-json export` | `export.go` |
| Restructure: the mapping, `Plan` (check, `Apply`, `JQ`, `Complete`), `writeNew`/`writeAtomic` | `restructure.go` |
| Reshape steps (the mapping's `then`): `Stage`, flatten (the common array, the tries, `prune`, rows and the arrays rule, leaf names), key-by, values, entries, lift (an array's items as numbered keys), each in Go (`stagePlan.apply`) and jq (`opJQ`); `NewDocPlan` chains them | `reshape.go` |
| Marks on the restructured tree (`node.mark`/`unmark`, `marks.*`, o-json's own by node id, `syncMarks` draws them), `marks.pick` (a tree edit), the reshape verbs (`marks.flatten`, `node.key-by`/`values`/`entries`) closing the tree into `rs.stages` (`reshape`, `stagedMapping`, `inShape`), their settings | `reshape_app.go` |
| The live jq panel (`jq-panel.*`, `jq.panel` split/modal/off, `jq.panel-height`): the program or the mapping in a read-only input (`ochazuke.ReadOnly`; the input owns the editor's keys; the viewer's page keys the editor does not bind (`R T M n N t / W`) shadowed by `ochazuke.key.ignore` rows in `jq-panel-text/normal` and `visual-line`, `TestJQPanelKeysChangeNothing`) | `jqpanel.go` |
| Value patches (the mapping's `patches`): `Patch`, the orphan rule, the Go apply (`applyPatches`, copies along the path) and the guarded jq steps | `patch.go` |
| Value edits in the data pane: the `value` noun (edit, delete, add, rename, paste, yank, patches, drop), the value under the cursor (`valueOf`), type inference (`inferValue`), the inline input (`value-input`, drawn on the row, `value.editor inline`), `data.editable`, a value edit during an off-loop rebuild (`rebuild.value-edit`: `onValue`, the queue `flushQueued` applies when the rebuild lands) | `values.go` |
| The value editor (`value.editor modal`, the default): ochazuke's prompt modal (`OpenModal` with its inputs, the body a `ModalBody`): the path title, the old value, the type badge, the key and value fields of an add, the problem line, `value-editor.*` | `valuemodal.go` |
| Stable table columns after a value edit (`table.stable-columns`: `stableColumns`, the anchors a rename or an add gives, the cursor following the edited key) | `columns.go` |
| The `doc` noun (JSON; `doc rows` answers rows with cells, so `--format` table, markdown and toon print tables) and `catalog` | `shell.go` |
| `view`, flags, `--smoke`, the program | `tui.go` |
| The empty document, `viewer.open`/`new`/`discard`/`paste`/`write`/`save`/`files`/`recent`, the recent list (`recent.json` in `Config.StateDirectory`, `recent.size`; the path chooser's copy `App.recent`), the before-quit check, the alias rows (`aliases`: `w` `w!` `wq` `x` `e` `q` `q!`), the viewer's `doc` noun (the open document: `doc.show`, and `docVerbs`, the viewer's document verbs under its name: `doc.open` is `viewer.open`), `empty.hint` | `open.go` |
| `App` state, `New`, `Load`/`setDoc`, the tree's nodes and filters, what the data panel shows | `app.go` |
| The windowed table (the list holds only the rows on screen), `rows.move`, column fitting (`fitColumns`, a kept column), the highlighted column or row (`litCol`, `litRow`), the lit column widened to its data (`measureLit`, `litCells`, `structure.lit-width`) | `rows.go` |
| What the data pane shows for the tree's selection (`dataTarget`, `display`): a collection is shown, a plain-value shape (or a list of plain values, `plainList`) keeps the collection that holds it and is highlighted there (`light`, `structure.scalar-follow`); the `object.view` setting | `follow.go` |
| The data pane's and the detail's views as data (`dataViews`, `detailViews`: name, the values each applies to, the rows it builds; `pickView`): a table, an object's one row, labeled fields. A new view is a row there, not a branch in `show` | `views.go` |
| Nested data in tables (`table.nested` hover): the sub-table per kind (`gridOf`), the open chain under the cursor (`hoverLevels`), the walk `rows.move` takes on the data pane (`hoverDown`/`hoverUp`) and `rows.column`, the drawing (`hoverView`, `hoverBlock`), the value under the cursor for `y`/`enter` (`hoverValue`) | `hover.go` |
| The page: breadcrumb, panels, the empty document's data pane (`emptyHint`), the divider (`PaneLayout` + `Divide`, so `ctrl+w >` `<` `=` resize) | `view.go` |
| Verbs | `verbs.go` |
| Key rows: `bindings.json` (embedded, one row per line; the only place a default key is written), loaded by `bindings.go` | `bindings.json`, `bindings.go` |
| The detail modal (drill in and out); o-json's default for ochazuke's `modal-highlights` (`plain`: the page beneath a modal drawn without its cursor bars) | `detail.go` |
| The restructure page: its two trees, node meta (origin by node id), the edits (send, indent, dedent, wrap, add-key, array/object, rename) as one undo step each, new keys opened selected with a placeholder, the origin highlight, the live mapping, write | `page.go` |
| Editing the structure in place: the viewer's structure pane is the restructured tree (lent to the open page, `placeTree`), the copy a document starts with (`identity`), the restructured document the data pane shows, `T` (`flip`), `structure.editable`, the restructure page's empty start | `inplace.go` |
| The wrap family: `node.wrap {as: array}` (the wrap and a `values` reshape step, `wrapAsArray`), `node.unwrap` (`unwrapObject`, a tree edit; an array's items through the `lift` reshape step), both one undo step through `reshapeFrom` (reshape_app.go: a tree edit not yet made closes into the steps) | `wrap.go` |
| Yank, delete and put on the restructured tree through ochazuke's registers (nodes as `o-json.nodes` text, or structure paths from the original's `y`, `pathBlocks`; a put keeps ids that are free, so delete + put is a move and yank + put a copy), where a put lands (`putPlace`), a put or wrap over a visual-line selection (`restructure.visual-put`, `restructure.visual-wrap`), dot-repeat's scope (`repeat.scope`) | `put.go` |
| The jq engine (gojq): `queryDoc` (path(Q) to the document's own nodes, else gojq's values), error positions, the builtins and their arity | `jq.go` |
| The live query: its input (the only query state), the runner (a Component: pause, one job slot), the answers, `jq.*` verbs, `/` focus and leave, `query.line` | `query.go` |
| The query line: the tokenizer (chips: field references, slots), drawing, the error caret | `queryline.go` |
| Query mode (`m`): the context at the anchor (`jqContextAt`), how a field is written there (`fieldTerm`), the jq-character verbs | `querymode.go` |
| The function picker and argument slots | `queryfunc.go` |
| Path completion after `.` in the query line (the trigger, the shape snapshot the off-loop search reads, `query.complete-paths`) | `querycomplete.go` |
| The story tester (`//go:build monogatari`): o-json as a monogatari app (`storyApp`, fixtures `nest`/`viewer`/`empty`, the seed's `doc`/`send`/`on`, the temp document and state dir), the `o-json story` mount (`host.Command`), the embedded stories and testdata documents; the normal build's empty mount; `host.Test` over `testdata/stories/<area>/<name>.json` | `story.go`, `story_off.go`, `story_test.go` |
| The job journeys: one story per common JSON job, open to quit (`testdata/stories/journeys`, its README lists them), on the shared sample `testdata/jobs` (orders.min.json, orders-broken.json, package.json) that `docs/tapes/gen-jobs-data.go` writes | `testdata/stories/journeys/`, `testdata/jobs/` |
| Where the Settings page files o-json's settings: the o-json tab, its sections and groups (`settingSections`), `declareOwn` (every o-json setting declares through it) | `settings_layout.go` |
| The search prompt, the system clipboard | `prompt.go`, `clipboard.go` |
| o-jj trial (Design D): the line views' records and keys (FLAT, JSON: `dataLine`, `line.yank-part`/`jump`/`fold`/`bracket`, `jumpStructure`) | `lines.go` |
| o-jj trial: the FLAT view's look (`flatPath`: depth tones `flat.depth.N`/`.N.dim` per path step, padded indexes; `flatLook`, `flat.depth-colors`, `flat.pad-indexes`); a line's `text` stays the plain path, `shown`/`tones` are what is drawn | `flat.go` |
| o-jj trial: the window keys (`viewer.only` per mode, `viewer.restore`, `viewer.pane` stopping at the edge) | `panes.go` |
| o-jj trial: the one search line (`searchTypes`: substring, fuzzy, regex, jq; the Results view; the overlay over the structure's view; `search.*`) | `search.go` |
| o-jj trial: EDIT (`viewer.edit`, `viewer.edit-leave`, `viewer.needs-edit`, `modes.edit-gate`) | `edit.go` |
| o-jj trial: RAW (`viewer.raw-edit`, the temp file, the foreground editor, `raw.editor`) | `raw.go` |
| o-jj trial: the mode picker (`g m`, `gotoMode`) and the declared modes (`syncDeclaredMode`, `dataViewOrder`, the trial accents) | `modepicker.go`, `views.go` |

## Invariants

- A repeated key reads as jq reads it, everywhere: the parser (`Build`) keeps one child at the first key's position with the last value, and `Node.Repeats` counts the repeats for the duplicate-key warning (`Shape.Duplicates`). Nothing reads the earlier values, so the viewer, the tables, `get`, the shape and `restructure` agree with jq. `TestEveryReadShowsTheLastValue` pins it.
- The source is never written unless asked: the viewer's panels write nothing, and the writes, `restructure --out`, `viewer.write`/`overwrite` and the page's `restructure.write`/`overwrite`/`export-map` (which refuse the source outright, `isSource`; only `restructure.overwrite-source`, which always confirms and which `W` runs for the source, writes it), go through `writeNew` (an existing file, the source among them, only with `--overwrite`) and `writeAtomic` (a temp file beside the target, then rename; copied from o-csv). `TestViewerNeverWritesTheSource`, `TestRestructureNeverWritesTheSource` and `TestWriteRefusesTheSource` pin this.
- Value edits are patches, never edits of the source or of the restructure's nodes: a list in the restructure state (`rs.patches`), part of the mapping (`Mapping().Patches`), so `w`, jq, `--map` and the data pane agree; each change is one `commitWith` step on the tree's undo stack (a structure `commit` keeps the patches as they are). A patch whose target is not there is orphaned (kept, skipped) in Go and in jq alike (`TestPatchesAgreeWithJQ`).
- Reshape steps make rows and keys out of values (`reshape.go`): the Go apply and the jq step agree on any document (`TestReshapeCasesAgreeWithJQ`, gojq and jq); a step's own map reads its output's shape, so a plan with steps is made on the document (`NewDocPlan`), never on a shape alone. Every undo step holds the reshape steps with the tree (`commitStaged`), so `u` brings a closed tree back.
- Restructuring maps shapes, not values (`restructure.go`): the Go result and the generated jq program read the document as jq does and must agree; the program never fails on an optional or mixed-type shape. `TestRestructureCasesAgreeWithJQ` runs it through `jq` when it is on PATH.
- Reads run off the loop: `viewer.open`, `viewer.load`, `viewer.paste` (the clipboard), `viewer.files`, `viewer.recent` and every command-line `doc` verb (shell.go; the viewer's `doc.show` reads no file) are `Pending`; the recent list is written on the read's goroutine. The `doc` verbs must stay pending: reading a document is I/O, kept off the loop. (Key order is not the reason any more; an immediate array would print one compact element per line and a failure would lose its `job #N doc.VERB failed` line.)
- Value edits work on the data pane and on the detail opened from the edited view (`editableValue`, `detailEditable`); the inline input and the value editor know their table (`valueEdit.view`), and the detail finds its trail again by path after an edit (`refreshDetail`).
- The data panel and the detail are `rowsView`s. Their ochazuke list holds the rows on screen and nothing more, and row IDs are positions in the window. Do not give a list one item per array element: ochazuke's List measures every row each frame and removes rows one instance scan at a time. Motions in those tables are `rows.move`, not `list.move`. The hover sub-tables (`hover.go`) are drawn into the data pane, never list rows: `rows.move` walks their lines itself, and the walk (`App.hover`) is dropped whenever the data pane's row, column or cell moves under it (`hoverCheck`).
- The tree is the structure (one node per shape), never the document, so its size is the number of distinct paths. Keep the `maxFields` cap: ochazuke's tree is quadratic in its node count.
- The panel's data follows the tree's selection in `render` (`sync`, through `display`: a plain-value shape keeps the holding collection and highlights itself, follow.go). Every way of moving the tree (keys, search, `tree.Select`) is covered by that, so do not add per-motion hooks.
- Every verb declares its history kind: `.Fetch()` on a read (`viewer.state`, the `doc` verbs), `setHistory(r, ochazuke.HistoryChange|HistoryFetch|HistoryAction, keys...)` for the rest (yanks are changes, moves and filters actions), so none falls to ochazuke's default. `TestEveryVerbDeclaresItsHistory` fails on a verb that declares none. The yanks are also `Operation.Repeatable` (`setRepeatable`), so `.` copies again at the focus.

- The restructured tree's first node is the document (no parent, path "."). It is identified by having no parent, never by its id, and every edit keeps it the only parentless node: an edit that would put a node beside it (dedent, wrap, a sibling key) does not apply. What a node refuses is its `TreeNode.Fixed`, set from data by `fixNodes` on every `SetNodes` (the document node: rename, move, delete; an item: rename), so ochazuke's own verbs refuse them too; o-json's guards read `Fixed` (`refuses`), not their own root checks.
- Quitting with edits no write saved asks through ochazuke's `BeforeQuit` check (`beforeQuit`); never wrap `app.quit`.
- The query line's completion search runs off the loop: it reads only the shape snapshot (`App.qc`, an atomic pointer stored while the line is drawn focused), never `App` state.
- One restructure state: the restructured tree (`rsTreeID`) is both the viewer's edited structure pane and the restructure page's right tree. It is one tree instance lent to whichever page is open (`placeTree`, from each page's `OnOpen`), never a second tree or a copy of its nodes, so the keys, the undo stack and the mapping are one. The viewer's original structure tree (`treeID`) and the page's original tree stay read-only.
- The restructure page's state is its mapping (`mappingOf` over the tree's nodes and `meta`): nothing else holds it, so the panel, `restructure.write` and `o-json restructure --map` agree (`TestLiveMappingIsWhatRestructureRuns`). A node's origin lives in `meta` by node id and is never dropped, so undo brings it back; o-json's edits go on the outline's undo stack as `TreeAction`s holding the nodes and `meta` before and after. A JSON rule is availability (`node-indent`, `node-dedent`), never a guard row.

- The query line's text is the only query state: results, chips, the anchor and the context all derive from it; runs are off the loop in one job slot (`TestQueryRunSupersededIsDropped`). A path query's results are the document's own nodes (`TestQueryPathKeepsOrder`); `o-json query` must agree with jq (`TestQueryCLIAgreesWithJQ`).

## Checks

`TestNoDeprecatedOchazuke` (ochazuke's `strictcheck`) fails on any deprecated ochazuke name (`ctx.Status`: use `ctx.Info` or `ctx.Error`). Give a verb availability (`.When(guard)`) rather than a guard on its rows.

Two builds, and the gate runs both (`make check`): `go vet` and `gofmt` see a tagged file only in the tagged run.

```sh
gofmt -l . && GOWORK=off go vet ./... && GOWORK=off go test -race ./...
GOWORK=off go vet -tags monogatari ./... && GOWORK=off go test -race -tags monogatari ./...
make split-check     # the normal binary has 0 monogatari symbols (go tool nm), the tester some
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

The tester (`make story-build`, `bin/o-json-story story run|open|explore ...`) links monogatari only behind the build tag `monogatari`: its files are `story.go` and `story_test.go`, and `story_off.go` (`!monogatari`) mounts nothing, so the shipped binary links none of it. Never import monogatari from an untagged file. gopls needs `-tags monogatari` in its build flags to edit the tagged files. A story picks its starting state with `fixture` (never `page`, which the o-json app does not read, so a story with one fails), its document with `seed`, and sets o-json's settings with `values` (`settings` declares new ones and fails on o-json's own). A UI bug becomes a story here (`"skip": "gap: ..."` until the fix) before it is fixed. The fixtures build the real viewer through `New`; a document is copied to a temp dir and the state directory is a temp dir, so no story reaches the repo's testdata or the user's state.

The race suite takes about two minutes. To time it, or to keep each run short on a busy machine, `scripts/test-chunks.sh OUT [N] [flags]` runs the top-level tests in N `go test -json` chunks (`scripts/test-chunks.sh /tmp/o-json-race 4 -race`, then `OUT/done`) and `python3 scripts/slowest.py OUT 15` prints failures, the total and the slowest tests (it also reads one `go test -json` file). Keep the suite fast: a selection walk (`selectIn`, `selectPath`) and `press` draw one frame where they stop, not one per key; tests that open their own viewer may `t.Parallel()` (registers, settings and history are per runtime); a test that needs a large document sizes it for `raceOn` (`rebuildWindowItems`), and `TestMain` fails a run in which every rebuild-window test skipped.

ochazuke (`v0.55.0`), oden (`v0.1.1`) and monogatari (`v0.1.0`, the tester only) are required at their tags. An untracked `go.work` (`go work init . ../ochazuke ../oden ../monogatari`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does. Never commit `go.work` or a `replace`.
