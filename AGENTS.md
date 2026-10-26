# o-json: agent guide

`o-json` (so `o json`) is the family's JSON structure and table viewer. It has a command line (`structure`, `table` and `get` print text, and the `doc` noun prints JSON for a host) and a two-panel ochazuke viewer. It is read-only. Usage and keys are in README.md.

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
| The text commands `structure`, `table`, `get` | `commands.go` |
| The `doc` noun (JSON) and `catalog`; the `quietJobs` stderr filter | `shell.go` |
| `view`, flags, `--smoke`, the program | `tui.go` |
| `App` state, `New`, `Load`/`setDoc`, the tree's nodes and filters, what the data panel shows | `app.go` |
| The windowed table (the list holds only the rows on screen), `rows.move`, column fitting | `rows.go` |
| The page: breadcrumb, panels | `view.go` |
| Verbs | `verbs.go` |
| Key rows | `bindings.go` |
| The detail modal (drill in and out) | `detail.go` |
| The search prompt, the system clipboard | `prompt.go`, `clipboard.go` |

## Invariants

- Read-only: no code path writes a file. If one is ever added, write atomically (a temp file beside the target, then rename), as o-csv's `writeAtomic` does. `TestReadOnlyNeverWrites` pins this.
- Reads run off the loop: `viewer.load` and every `doc` verb are `Pending`. The `doc` verbs must stay pending, because a synchronous result goes through a JSON map and loses the document's key order.
- The data panel and the detail are `rowsView`s. Their ochazuke list holds the rows on screen and nothing more, and row IDs are positions in the window. Do not give a list one item per array element: ochazuke's List measures every row each frame and removes rows one instance scan at a time. Motions in those tables are `rows.move`, not `list.move`.
- The tree is the structure (one node per shape), never the document, so its size is the number of distinct paths. Keep the `maxFields` cap: ochazuke's tree is quadratic in its node count.
- The panel's data follows the tree's selection in `render` (`sync`). Every way of moving the tree (keys, search, `tree.Select`) is covered by that, so do not add per-motion hooks.

## Checks

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

ochazuke (`v0.2.0`) and oden (`v0.1.1`) are required at their tags. An untracked `go.work` (`go work init . ../ochazuke ../oden`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does. Never commit `go.work` or a `replace`.
