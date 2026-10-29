# o-json: agent guide

`o-json` (so `o json`) is the family's JSON structure and table viewer. It has a command line (`structure`, `table` and `get` print text, and the `doc` noun prints JSON for a host) and a two-panel ochazuke viewer. It never writes the document it reads unless asked: only `restructure --out`, the viewer's `viewer.write` and the restructure page's `restructure.write`/`export-map` write a file (the recent list, `recent.json` in the state directory, aside), a new one unless overwriting is asked for (and confirmed, on the page); the source only with `--overwrite` or the page's `W`, which confirms (`restructure.overwrite-source`). Usage and keys are in README.md.

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
| Restructure: the mapping, `Plan` (check, `Apply`, `JQ`, `Complete`), `writeNew`/`writeAtomic` | `restructure.go` |
| The `doc` noun (JSON; `doc rows` answers rows with cells, so `--format` table, markdown and toon print tables) and `catalog` | `shell.go` |
| `view`, flags, `--smoke`, the program | `tui.go` |
| The empty document, `viewer.open`/`new`/`discard`/`paste`/`write`/`files`/`recent`, the recent list (`recent.json` in `Config.StateDirectory`, `recent.size`) | `open.go` |
| `App` state, `New`, `Load`/`setDoc`, the tree's nodes and filters, what the data panel shows | `app.go` |
| The windowed table (the list holds only the rows on screen), `rows.move`, column fitting | `rows.go` |
| The page: breadcrumb, panels, the divider (`PaneLayout` + `Divide`, so `>` `<` `=` resize) | `view.go` |
| Verbs | `verbs.go` |
| Key rows: `bindings.json` (embedded, one row per line; the only place a default key is written), loaded by `bindings.go` | `bindings.json`, `bindings.go` |
| The detail modal (drill in and out) | `detail.go` |
| The restructure page: its two trees, node meta (origin by node id), the edits (send, indent, dedent, wrap, add-key, array/object, rename) as one undo step each, new keys opened selected with a placeholder, the origin highlight, the live mapping, write | `page.go` |
| Yank, delete and put on the restructured tree through ochazuke's registers (nodes as `o-json.nodes` text; a put keeps ids that are free, so delete + put is a move and yank + put a copy), where a put lands (`putPlace`), the V selection's `d`/`y` | `put.go` |
| The search prompt, the system clipboard | `prompt.go`, `clipboard.go` |

## Invariants

- A repeated key reads as jq reads it, everywhere: the parser (`Build`) keeps one child at the first key's position with the last value, and `Node.Repeats` counts the repeats for the duplicate-key warning (`Shape.Duplicates`). Nothing reads the earlier values, so the viewer, the tables, `get`, the shape and `restructure` agree with jq. `TestEveryReadShowsTheLastValue` pins it.
- The source is never written unless asked: the viewer's panels write nothing, and the writes, `restructure --out`, `viewer.write`/`overwrite` and the page's `restructure.write`/`overwrite`/`export-map` (which refuse the source outright, `isSource`; only `restructure.overwrite-source`, which always confirms and which `W` runs for the source, writes it), go through `writeNew` (an existing file, the source among them, only with `--overwrite`) and `writeAtomic` (a temp file beside the target, then rename; copied from o-csv). `TestViewerNeverWritesTheSource`, `TestRestructureNeverWritesTheSource` and `TestWriteRefusesTheSource` pin this.
- Restructuring maps shapes, not values (`restructure.go`): the Go result and the generated jq program read the document as jq does and must agree; the program never fails on an optional or mixed-type shape. `TestRestructureCasesAgreeWithJQ` runs it through `jq` when it is on PATH.
- Reads run off the loop: `viewer.open`, `viewer.load`, `viewer.paste` (the clipboard), `viewer.files`, `viewer.recent` and every `doc` verb are `Pending`; the recent list is written on the read's goroutine. The `doc` verbs must stay pending: reading a document is I/O, kept off the loop. (Key order is not the reason any more; an immediate array would print one compact element per line and a failure would lose its `job #N doc.VERB failed` line.)
- The data panel and the detail are `rowsView`s. Their ochazuke list holds the rows on screen and nothing more, and row IDs are positions in the window. Do not give a list one item per array element: ochazuke's List measures every row each frame and removes rows one instance scan at a time. Motions in those tables are `rows.move`, not `list.move`.
- The tree is the structure (one node per shape), never the document, so its size is the number of distinct paths. Keep the `maxFields` cap: ochazuke's tree is quadratic in its node count.
- The panel's data follows the tree's selection in `render` (`sync`). Every way of moving the tree (keys, search, `tree.Select`) is covered by that, so do not add per-motion hooks.
- Every verb declares its history kind: `.Fetch()` on a read (`viewer.state`, the `doc` verbs), `setHistory(r, ochazuke.HistoryChange|HistoryFetch|HistoryAction, keys...)` for the rest (yanks are changes, moves and filters actions), so none falls to ochazuke's default. `TestEveryVerbDeclaresItsHistory` fails on a verb that declares none. The yanks are also `Operation.Repeatable` (`setRepeatable`), so `.` copies again at the focus.

- The restructured tree's first node is the document (no parent, path "."). It is identified by having no parent, never by its id, and every edit keeps it the only parentless node: an edit that would put a node beside it (dedent, wrap, a sibling key) does not apply.
- The restructure page's state is its mapping (`mappingOf` over the tree's nodes and `meta`): nothing else holds it, so the panel, `restructure.write` and `o-json restructure --map` agree (`TestLiveMappingIsWhatRestructureRuns`). A node's origin lives in `meta` by node id and is never dropped, so undo brings it back; o-json's edits go on the outline's undo stack as `TreeAction`s holding the nodes and `meta` before and after. A JSON rule is availability (`node-indent`, `node-dedent`), never a guard row.

## Checks

`TestNoDeprecatedOchazuke` (ochazuke's `strictcheck`) fails on any deprecated ochazuke name (`ctx.Status`: use `ctx.Info` or `ctx.Error`). Give a verb availability (`.When(guard)`) rather than a guard on its rows.

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

ochazuke (`v0.38.0`) and oden (`v0.1.1`) are required at their tags. An untracked `go.work` (`go work init . ../ochazuke ../oden`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does. Never commit `go.work` or a `replace`.
