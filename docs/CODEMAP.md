# Code map

The root package is the CLI and viewer. `AGENTS.md` has the complete subsystem
map; this file records the document lifecycle shared by its entry points.

| File | Responsibility |
|---|---|
| `main.go` | Oden dispatch and the leading `--data` state directory. |
| `shell.go` | JSON read commands, shared document store registration and catalog page declaration. `doc get FILE PATH` reads JSON; `doc get --id ID` reads its record. |
| `tui.go` | Standalone viewer, actual terminal loop and one-frame smoke mode. |
| `app.go`, `open.go` | Viewer buffer, document reads, current-file association, recent files and existing write/unsaved-edit safety. |
| `stores.go` | Read-only facts for transient state and the rendered row window. JSON addresses remain strings, not file paths. |
| `document_store.go` | Shared typed document and annotation declarations, remote store calls, source validation and real file create/copy/move/rename/content/trash mutations. |
| `document_move.go` | No-replace disk moves, including exclusive copy and sync across filesystems. |
| `document_state.go` | Metadata, notes and relations in `documents.json`; filesystem transitions and identity updates commit under one advisory file lock, with rollback before unlocking on failure. In-memory state with disabled test configuration. |
| `document_viewer.go` | Viewer adoption, import of a successfully parsed file, and current-file updates after a disk move. |
| `document_actions.go` | Registered native viewer open, filesystem actions, typed host pane/note/project calls, reciprocal actions and JSON catalog page using the framework store-backed index. |
| `document_trash.go` | System Trash command selection and injectable command runner. |
| `document_actions_test.go` | File lifecycle, stable identity, persistence, source-safety and Trash-runner behavior. |
| `document_atomic_test.go` | Exclusive publication, contending creates/duplicates, move/import identity serialization, commit-failure rollback and native stdin compatibility. |
| `document_page_test.go` | Catalog index declaration, absence of a redundant listing action, store-backed record selection and hydrated JSON detail across opens. |
| `restructure.go` | Shared synced atomic writer: replace explicit writes by rename, publish new files by exclusive hard link. |
| `raw.go` | Existing editor preference order, reused by document editor-pane actions. |
| `cheatsheet_test.go`, `scripts/cheatsheet.sh` | Repository-owned generated key reference in `docs/GUIDE.md`. |

The host owns panes, host notes and projects. Tool actions call `cli.Host.Call`
through `OCHAZUKE_HOST`. Ochazuke owns generated forms, history/undo, relationships,
typed value verbs, mounted record hydration and reciprocal action registration.
No tool action connects directly to a daemon.
