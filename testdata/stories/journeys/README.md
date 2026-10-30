# Job journeys

One story per common JSON job (`~/hub/test-archive/2026-10-30/o-json-jobs/what-people-do-with-json.md`, in its order), each a whole session from opening the file to quitting. Each step has an expectation, and the `notes` field narrates it (one `{after, keys, note}` per step). They all use the shared sample in `testdata/jobs/` (`go run docs/tapes/gen-jobs-data.go testdata/jobs` regenerates it).

| Journey | Job | Opens with | Keys it walks | Events |
|---|---|---|---|---|
| `01-read-minified-api-dump` | 1 read a minified API dump | `e`, `ctrl+o` recent, typed name | `j` `ctrl+w l` `ctrl+d` `G` `g g` `tab` (FLAT, JSON) `z c` `z M` `z R` `%` `3 j y y` `ctrl+w o` `ctrl+w =` `ctrl+w h` `q` | 37 |
| `02-understand-the-shape` | 2 understand the shape | `o-json orders.min.json` (viewer fixture) | `T` (counts, `email?`) `t` `t` `esc` `g g 7 j` `y p` `enter` (fold) `ctrl+w l $` `tab` (missing vs null) `T` `g m` `esc` `q` | 25 |
| `03-find-a-key-or-value-anywhere` | 3 find a key or value | `:e`, recent | `/refunded` `enter` `n n N` `y p` `enter` `esc` `/cpn` `tab` (fuzzy) `enter` `j y y` `G` `esc` `q` | 28 |
| `04-copy-the-path-to-a-value` | 4 copy a path | viewer fixture | `y p` `y a p` `"a y i p` `l j` (NESTED) `l y p` `esc` `! i p` (jq) `enter` `esc` `tab 5 j y p` `y v` `q` | 37 |
| `05-filter-and-extract-with-jq` | 5 filter or extract with jq | bracketed paste (a copied response) | `/ shift+tab` (jq) `select(...)` `enter` `/` `\| {id, total, who}` `enter` `l l y` `j y` `ctrl+w o` `ctrl+w =` `esc` `! i c` `enter` `esc` `q` | 30 |
| `06-table-sort-and-export` | 6 table: read, sort, export | viewer fixture | `4 l j` (items) `j $ y p` `esc` `s i c` `s s` `j j j` `0 enter` (detail) `q` `w orders-by-total.csv` `:sort` (clears on `#`) `q` | 31 |
| `07-edit-a-config-file` | 7 edit a config file | viewer fixture (`package.json`) | `x` (refused) `i` `r` `ctrl+u` `1.5.0` `u` `ctrl+r` `esc` `8 j` `i o zod tab ^3.23.8` `s i o` `D` `w package.edited.json` `i x` `q` (asks) `n` `Q` | 41 |
| `08-find-where-a-file-is-broken` | 8 find where a file is broken | `e`, recent: the broken file fails | the error's line and column, paste the excerpt (same error), paste it fixed, `4 l` `tab` `6 j y p` `tab` `T` `/price` `esc q` | 26 |
| `13-reindent-a-minified-file` | 13 re-indent (minify and sort keys are gaps) | `e`, recent | `tab tab tab` (JSON) `j z c` `z R` `G` `g g` `T` `w alt+o orders.pretty.json` `T` `q` | 27 |
| `14-count-and-group-with-jq` | 14 count and group (jq) | viewer fixture | `/ shift+tab` `[... select(.coupon)] \| length` `group_by(.status)` `sort_by(-.orders)` revenue per status `j l l y` `ctrl+w o` `ctrl+w =` `esc` `q` | 30 |

Jobs 9 (diff), 10 (YAML/TOML), 11 (JSON Lines), 12 (large files) and 15 (schema) have no journey: o-json cannot do them yet.

## Watching them

```sh
make story-build
bin/o-json-story story open journeys/01-read-minified-api-dump    # live, in the real chrome
bin/o-json-story story run -v journeys                            # headless; -v prints a failing step's frame
bin/o-json-story story story show --target journeys/07-edit-a-config-file   # the story, its notes included
go test -tags monogatari -run 'TestStories/journeys' .
```

## Bugs the journeys route around

- `/` (or `!`) pressed on the data pane: the Results or jq result view shows but the badge stays TABLE, so the table's keys answer (`y y` yanks the whole `path │ value` line). The line restores the mode it was opened from (`query.go:376-377`) after `search.accept` focuses the data pane (`search.go:256-260`). Journey 03 opens its second search from the tree; journey 05 says so in a note.
- `:sort` / `s i c` on a jq result table fails with "s: no column here" (`operate.go:440-442`). Journey 14 sorts in jq.
