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
| Value patches (the mapping's `patches`): `Patch`, the orphan rule, the Go apply (`applyPatches`, copies along the path) and the guarded jq steps | `patch.go` |
| Value edits in the data pane: the `value` noun (edit, delete, add, rename, paste, yank, patches, drop), the value under the cursor (`valueOf`), type inference (`inferValue`), the inline input (`value-input`, drawn on the row), `data.editable`, a value edit during an off-loop rebuild (`rebuild.value-edit`: `onValue`, the queue `flushQueued` applies when the rebuild lands) | `values.go` |
| The `doc` noun (JSON; `doc rows` answers rows with cells, so `--format` table, markdown and toon print tables) and `catalog` | `shell.go` |
| `view`, flags, `--smoke`, the program | `tui.go` |
| The empty document, `viewer.open`/`new`/`discard`/`paste`/`write`/`save`/`files`/`recent`, the recent list (`recent.json` in `Config.StateDirectory`, `recent.size`; the path chooser's copy `App.recent`), the before-quit check, the alias rows (`aliases`: `w` `w!` `wq` `x` `e` `q` `q!`) | `open.go` |
| `App` state, `New`, `Load`/`setDoc`, the tree's nodes and filters, what the data panel shows | `app.go` |
| The windowed table (the list holds only the rows on screen), `rows.move`, column fitting | `rows.go` |
| Nested data in tables (`table.nested` hover): the sub-table per kind (`gridOf`), the open chain under the cursor (`hoverLevels`), the walk `rows.move` takes on the data pane (`hoverDown`/`hoverUp`) and `rows.column`, the drawing (`hoverView`, `hoverBlock`), the value under the cursor for `y`/`enter` (`hoverValue`) | `hover.go` |
| The page: breadcrumb, panels, the divider (`PaneLayout` + `Divide`, so `ctrl+w >` `<` `=` resize) | `view.go` |
| Verbs | `verbs.go` |
| Key rows: `bindings.json` (embedded, one row per line; the only place a default key is written), loaded by `bindings.go` | `bindings.json`, `bindings.go` |
| The detail modal (drill in and out) | `detail.go` |
| The restructure page: its two trees, node meta (origin by node id), the edits (send, indent, dedent, wrap, add-key, array/object, rename) as one undo step each, new keys opened selected with a placeholder, the origin highlight, the live mapping, write | `page.go` |
| Editing the structure in place: the viewer's structure pane is the restructured tree (lent to the open page, `placeTree`), the copy a document starts with (`identity`), the restructured document the data pane shows, `T` (`flip`), `structure.editable`, the restructure page's empty start | `inplace.go` |
| Yank, delete and put on the restructured tree through ochazuke's registers (nodes as `o-json.nodes` text, or structure paths from the original's `y`, `pathBlocks`; a put keeps ids that are free, so delete + put is a move and yank + put a copy), where a put lands (`putPlace`), a put or wrap over a visual-line selection (`restructure.visual-put`, `restructure.visual-wrap`), dot-repeat's scope (`repeat.scope`) | `put.go` |
| The jq engine (gojq): `queryDoc` (path(Q) to the document's own nodes, else gojq's values), error positions, the builtins and their arity | `jq.go` |
| The live query: its input (the only query state), the runner (a Component: pause, one job slot), the answers, `jq.*` verbs, `/` focus and leave, `query.line` | `query.go` |
| The query line: the tokenizer (chips: field references, slots), drawing, the error caret | `queryline.go` |
| Query mode (`m`): the context at the anchor (`jqContextAt`), how a field is written there (`fieldTerm`), the jq-character verbs | `querymode.go` |
| The function picker and argument slots | `queryfunc.go` |
| Path completion after `.` in the query line (the trigger, the shape snapshot the off-loop search reads, `query.complete-paths`) | `querycomplete.go` |
| o-json's stories (`testdata/stories/<area>/<name>.json`) run through `ochazuke/story` against the real viewer | `story_test.go` |
| The search prompt, the system clipboard | `prompt.go`, `clipboard.go` |

## Invariants

- A repeated key reads as jq reads it, everywhere: the parser (`Build`) keeps one child at the first key's position with the last value, and `Node.Repeats` counts the repeats for the duplicate-key warning (`Shape.Duplicates`). Nothing reads the earlier values, so the viewer, the tables, `get`, the shape and `restructure` agree with jq. `TestEveryReadShowsTheLastValue` pins it.
- The source is never written unless asked: the viewer's panels write nothing, and the writes, `restructure --out`, `viewer.write`/`overwrite` and the page's `restructure.write`/`overwrite`/`export-map` (which refuse the source outright, `isSource`; only `restructure.overwrite-source`, which always confirms and which `W` runs for the source, writes it), go through `writeNew` (an existing file, the source among them, only with `--overwrite`) and `writeAtomic` (a temp file beside the target, then rename; copied from o-csv). `TestViewerNeverWritesTheSource`, `TestRestructureNeverWritesTheSource` and `TestWriteRefusesTheSource` pin this.
- Value edits are patches, never edits of the source or of the restructure's nodes: a list in the restructure state (`rs.patches`), part of the mapping (`Mapping().Patches`), so `w`, jq, `--map` and the data pane agree; each change is one `commitWith` step on the tree's undo stack (a structure `commit` keeps the patches as they are). A patch whose target is not there is orphaned (kept, skipped) in Go and in jq alike (`TestPatchesAgreeWithJQ`).
- Restructuring maps shapes, not values (`restructure.go`): the Go result and the generated jq program read the document as jq does and must agree; the program never fails on an optional or mixed-type shape. `TestRestructureCasesAgreeWithJQ` runs it through `jq` when it is on PATH.
- Reads run off the loop: `viewer.open`, `viewer.load`, `viewer.paste` (the clipboard), `viewer.files`, `viewer.recent` and every `doc` verb are `Pending`; the recent list is written on the read's goroutine. The `doc` verbs must stay pending: reading a document is I/O, kept off the loop. (Key order is not the reason any more; an immediate array would print one compact element per line and a failure would lose its `job #N doc.VERB failed` line.)
- Value edits work on the data pane and on the detail opened from the edited view (`editableValue`, `detailEditable`); the inline input knows its table (`valueEdit.view`), and the detail finds its trail again by path after an edit (`refreshDetail`).
- The data panel and the detail are `rowsView`s. Their ochazuke list holds the rows on screen and nothing more, and row IDs are positions in the window. Do not give a list one item per array element: ochazuke's List measures every row each frame and removes rows one instance scan at a time. Motions in those tables are `rows.move`, not `list.move`. The hover sub-tables (`hover.go`) are drawn into the data pane, never list rows: `rows.move` walks their lines itself, and the walk (`App.hover`) is dropped whenever the data pane's row, column or cell moves under it (`hoverCheck`).
- The tree is the structure (one node per shape), never the document, so its size is the number of distinct paths. Keep the `maxFields` cap: ochazuke's tree is quadratic in its node count.
- The panel's data follows the tree's selection in `render` (`sync`). Every way of moving the tree (keys, search, `tree.Select`) is covered by that, so do not add per-motion hooks.
- Every verb declares its history kind: `.Fetch()` on a read (`viewer.state`, the `doc` verbs), `setHistory(r, ochazuke.HistoryChange|HistoryFetch|HistoryAction, keys...)` for the rest (yanks are changes, moves and filters actions), so none falls to ochazuke's default. `TestEveryVerbDeclaresItsHistory` fails on a verb that declares none. The yanks are also `Operation.Repeatable` (`setRepeatable`), so `.` copies again at the focus.

- The restructured tree's first node is the document (no parent, path "."). It is identified by having no parent, never by its id, and every edit keeps it the only parentless node: an edit that would put a node beside it (dedent, wrap, a sibling key) does not apply. What a node refuses is its `TreeNode.Fixed`, set from data by `fixNodes` on every `SetNodes` (the document node: rename, move, delete; an item: rename), so ochazuke's own verbs refuse them too; o-json's guards read `Fixed` (`refuses`), not their own root checks.
- Quitting with edits no write saved asks through ochazuke's `BeforeQuit` check (`beforeQuit`); never wrap `app.quit`.
- The query line's completion search runs off the loop: it reads only the shape snapshot (`App.qc`, an atomic pointer stored while the line is drawn focused), never `App` state.
- One restructure state: the restructured tree (`rsTreeID`) is both the viewer's edited structure pane and the restructure page's right tree. It is one tree instance lent to whichever page is open (`placeTree`, from each page's `OnOpen`), never a second tree or a copy of its nodes, so the keys, the undo stack and the mapping are one. The viewer's original structure tree (`treeID`) and the page's original tree stay read-only.
- The restructure page's state is its mapping (`mappingOf` over the tree's nodes and `meta`): nothing else holds it, so the panel, `restructure.write` and `o-json restructure --map` agree (`TestLiveMappingIsWhatRestructureRuns`). A node's origin lives in `meta` by node id and is never dropped, so undo brings it back; o-json's edits go on the outline's undo stack as `TreeAction`s holding the nodes and `meta` before and after. A JSON rule is availability (`node-indent`, `node-dedent`), never a guard row.

- The query line's text is the only query state: results, chips, the anchor and the context all derive from it; runs are off the loop in one job slot (`TestQueryRunSupersededIsDropped`). A path query's results are the document's own nodes (`TestQueryPathKeepsOrder`); `o-json query` must agree with jq (`TestQueryCLIAgreesWithJQ`).

## Checks

`TestNoDeprecatedOchazuke` (ochazuke's `strictcheck`) fails on any deprecated ochazuke name (`ctx.Status`: use `ctx.Info` or `ctx.Error`). Give a verb availability (`.When(guard)`) rather than a guard on its rows.

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

The race suite takes about two minutes. To time it, or to keep each run short on a busy machine, `scripts/test-chunks.sh OUT [N] [flags]` runs the top-level tests in N `go test -json` chunks (`scripts/test-chunks.sh /tmp/o-json-race 4 -race`, then `OUT/done`) and `python3 scripts/slowest.py OUT 15` prints failures, the total and the slowest tests (it also reads one `go test -json` file). Keep the suite fast: a selection walk (`selectIn`, `selectPath`) and `press` draw one frame where they stop, not one per key; tests that open their own viewer may `t.Parallel()` (registers, settings and history are per runtime); a test that needs a large document sizes it for `raceOn` (`rebuildWindowItems`), and `TestMain` fails a run in which every rebuild-window test skipped.

ochazuke (`v0.46.0`) and oden (`v0.1.1`) are required at their tags. An untracked `go.work` (`go work init . ../ochazuke ../oden`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does. Never commit `go.work` or a `replace`.
