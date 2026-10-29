# o-json

A JSON structure and table viewer for the o family, built on [ochazuke](../ochazuke) (Bubble Tea v2) with an [oden](../oden) command line. It answers two questions about a document: what shape is it (every path, its types, how many values have it, which fields are optional), and what does an array of objects look like as a table. It never writes the document it reads: the one command that writes, `restructure --out`, makes a new file and replaces an existing one only with `--overwrite`.

```sh
o-json data.json                        # open the viewer (also: o-json view data.json)
curl -s https://... | o-json            # or read stdin (also: o-json -)
o-json structure data.json              # the shape as a tree
o-json table data.json .items           # an array as a table, optional columns last
o-json get data.json '.items[0].owner'  # one value
o-json restructure data.json --map spec.json        # a new document from a mapping
o-json restructure data.json --map spec.json --jq   # the same as a jq program
o-json --smoke data.json                # render one 80x24 frame to stdout and exit
```

Build: `go build -o ~/bin/o-json .`

## Paths

Paths are jq style: `.` is the whole document, `.key` and `["odd key"]` step into an object, `[2]` (or `[-1]`, from the end) into an array, and `[]` means every item. The leading dot may be left off (`items[0].name`). A path with `[]` names many values. The structure uses `[]` paths, so `.items[].owner` is the shape of every item's owner.

## Structure

One pass over the document records, for every `[]` path, the types seen, how many values have it, and, for an object field, how many of the objects at its parent have it. A field is optional when some of those objects lack it. Fields are listed most common first, ties in the order they first appear, so optional fields come after the shared ones.

```
$ o-json structure testdata/sample.json
.               object         1
  name          string       1/1
  version       number       1/1
  items         array        1/1  4 items
    []          object         4
      id        number       4/4
      name      string       4/4
      owner     object       4/4
        name    string       4/4
        email   string|null  2/4  optional
      tags      array        4/4  0..2 items
        []      string         4
      archived  bool         1/4  optional
      note      string       1/4  optional
  meta          object       1/1
    generated   string       1/1
    count       number       1/1
    source      null         1/1
```

A key an object repeats (`{"id": 3, "id": 33}`) counts once for that object, so the count and the optional flag stay true. Every read takes the last value at the first key's position, as jq (and any parser that builds a map) does: the viewer, the tables, `get`, the shape and `restructure` all agree with jq. The structure notes `duplicate key (N)`, N being the most times one object has it, and the viewer tones the field's name with the error colour and says `duplicate key "id" (2): jq keeps the last` on the breadcrumb.

`--format json` prints the same tree as JSON (`{key, path, count, of, types, min_items, max_items, optional, duplicates: {objects, most}, children}`).

## Tables

`o-json table FILE [PATH]` lays out an array (or the values a `[]` path names) with one row per item and one column per key any object item has, in the same frequency order, so optional columns are the rightmost. A missing field is a blank cell, which is not the same as `null`. Items that are not objects fill a `(value)` column. An optional column's title ends in `?`. Objects and arrays inside a cell are compact JSON.

```
$ o-json table testdata/sample.json .items
id  name   owner                                     tags            archived?  note?
1   alpha  {"name":"ann","email":"ann@example.com"}  ["red","blue"]
2   beta   {"name":"bob"}                            []              true
3   gamma  {"name":"cy","email":null}                ["green"]                  third
4   delta  {"name":"dee"}                            ["red"]

$ o-json table --format markdown testdata/sample.json '.items[].owner'
| name | email? |
| --- | --- |
| ann | ann@example.com |
| bob |  |
| cy | null |
| dee |  |
```

`--format json` prints `{path, columns: [{name, count, optional, types, self}], rows}`, with the rows as they are in the document.

## get

`o-json get FILE PATH` prints the value as indented JSON with its keys in document order. A `[]` path prints each value it names on its own (`--format json` makes them one array), and `--raw` prints strings without quotes.

```
$ o-json get testdata/sample.json '.items[].owner.name' --raw
ann
bob
cy
dee
```

## restructure

`o-json restructure FILE --map SPEC.json` makes a new document from a mapping of structure paths. It works on shapes: an entry names `.items[].owner.name`, not one item's, so it maps that field of every item.

A mapping is `{"map": [entries]}` and an entry is `{"from": PATH, "to": PATH}`. `from` is a path the structure lists; `to` is the path in the new document, in the same syntax (keys and `[]`, no indexes). An entry without `from` makes a new object (a wrapper), and `"to": null` drops its `from`. The `to` paths make a tree:

- A node with children is built from them: an object from its keys (in the order the mapping first names them), an array from its `[]` item.
- A node without children copies the value at its `from`, keys in document order.
- A `[]` item needs a `from`: the values it is made of. Inside it, every `from` lies under the item's own `from`, so `{"from": ".items[]", "to": ".records[]"}` with `{"from": ".items[].owner.name", "to": ".records[].owner"}` makes one record per item. A `[]` item's `from` may fan out further: `{"from": ".items[].tags[]", "to": ".all_tags[]"}` collects every item's tags. A key takes one value, so a key's `from` may not.
- A field is copied only where it is present. An optional field gives its key to the objects that have it and no others; a step into a value of another type (`.owner.name` where `owner` is sometimes a string) counts as absent, never as an error. A wrapper is always there, even empty.

```json
{"map": [
  {"from": ".version", "to": ".v"},
  {"from": ".items[]", "to": ".records[]"},
  {"from": ".items[].id", "to": ".records[].id"},
  {"to": ".records[].info"},
  {"from": ".items[].name", "to": ".records[].info.name"},
  {"from": ".items[].note", "to": ".records[].info.note"},
  {"from": ".items[].owner.name", "to": ".records[].owner"},
  {"from": ".items[].tags[]", "to": ".all_tags[]"},
  {"from": ".items[].archived", "to": null}
]}
```

`--jq` prints a jq program that makes the same document from any document of this shape, and never fails on it: an iteration is `[]?` (or a type test where the values may be objects), an optional field is copied under `if has("k")`, and a step into a value that may not be an object tests its type first. For `testdata/restructure.json` and `testdata/restructure.map.json`:

```jq
{"v": .version, "records": [.items[] | {"id": .id, "info": ({"name": .name} + (if has("note") then {"note": .note} else {} end))} + (if has("size") then {"size": .size} else {} end) + (if (.owner | type == "object") then {"owner": .owner.name} else {} end) + {"contact": (if (.owner | type == "object" and has("email")) then {"email": .owner.email} else {} end), "tag_names": [select(has("tags")) | .tags[]?.t]}], "all_tags": [.items[] | select(has("tags")) | .tags[]?], "names": [.items[].name]}
```

`--mapping` prints the mapping with every shape it leaves out added as a drop (`{"from": ".items[].archived", "to": null}`), so it reads as the whole story of the source; the output is itself a mapping.

A key an object repeats is read as jq reads it: the last value, at the first one's position. The Go result and the jq program therefore agree, though the viewer and `table` show the first value.

Output goes to stdout. `--out PATH` writes it to a new file instead (atomically: a temporary file beside it, then a rename); an existing file, the source among them, is replaced only with `--overwrite`. `--out` takes a value: the flag parser has no optional values, so there is no default `NAME.restructured.json`.

## For a host: the doc noun and the catalog

`o-json doc structure|table|get FILE [PATH]` gives the same answers as JSON through ochazuke's `cli.Shell`, and `o-json catalog` describes the noun, so `o-nabe extension register json` can mount it. A value keeps the document's key order.

`o-json doc rows FILE [PATH]` is `doc table`'s rows for a host and for the formats: JSON Lines `{noun: "item", id: PATH, data: ITEM, cells}`, the item in document order and one cell per table column. `--format table|markdown|toon` prints them as a table under `doc table`'s columns (an optional column's title ends in `?`, numbers are right-aligned, and on a narrow terminal the optional columns drop first):

```
$ o-json doc rows testdata/sample.json '.items[].owner' --format table
name  email?
ann   ann@example.com
bob
cy    null
dee
```

`doc get` of an array of objects prints under `--format` too (its fields as columns). `doc structure` and `doc table` answer one object, so `--format` other than json prints their JSON with a note on stderr.

## The viewer

The viewer has a breadcrumb line and then two panels. The breadcrumb shows the file, the selected path with its types and presence, the selected row's own path when the data panel holds the focus, and the active filters.

- **Left: the structure tree**, ochazuke's outline, read-only. Each row is a path with its types and count, and an optional field's name ends in `?`. A structure with more than 150 paths opens collapsed below the second level, and an object with more than 200 distinct keys (a map) lists the first 200 and a `… N more fields` row.
- **Right: the selected path's data.** One array, or the values of a `[]` path, is a table with a `#` column, columns in frequency order and optional ones last. One object is labeled `field  type  value` rows, and one scalar is shown as is. Column widths come from the first 2,000 rows, so they hold still while you scroll. On a narrow screen the widest columns narrow first, and after that columns drop from the right, so the optional columns go first. Only the rows on screen are in the table, so a 100,000-row array costs one screen per key.
- **Detail**: `enter` on a table row, or on a field that holds an object or an array, opens it in a modal as labeled rows. `enter` (or `l`) on a nested object or array steps into it, and `h` or `backspace` steps back out.

The file is read off the event loop (`viewer.load`), and the screen says `loading …` until it is done. A parse error gives the line and column. The panels sit side by side from 60 columns up and stack below that. `>` and `<` move the divider (the tree's width, or its height when stacked) and `=` puts it back; the divider is remembered in `~/.local/state/o-json/panes.json`.

### Keys

`?` lists every key the focus answers to, generated from the binding rows. The footer shows the keys that are not motions.

| Key | Where | Does |
|---|---|---|
| `j` `k`, arrows | anywhere | move (counts work: `5j`) |
| `l` / `h` | tree | expand, or step onto the first child / collapse, or step onto the parent |
| `enter` | tree | expand or collapse |
| `tab` / `shift+tab` | anywhere | the other panel |
| `>` / `<`, `=` | tree, data | grow / shrink the tree panel (counts work: `3>`), reset the divider |
| `g g` / `G`, `home` / `end` | data, detail | first / last row (`12G` goes to row 12) |
| `ctrl+d` `ctrl+u` / `ctrl+f` `ctrl+b`, `pgdown` `pgup` | data, detail | half a screen / a screen |
| `enter` | data | open the row's item (or the field's object or array) in the detail |
| `h` `l`, `left` `right`, `0` `^` / `$`, `N\|`, `g c` | data, detail | move the current cell's column: one left / right (in the detail `h` and `l` are the entry's), first / last, column N, choose one |
| `z >` / `z <`, `z =`, `z p` | data, detail | widen / narrow the current column, make every column automatic, pin the current column |
| `enter` `l` / `h` `backspace` | detail | step into the nested value / back out (at the top, close) |
| `q`, `esc` | detail | close |
| `/` | tree, data | search the paths: the tree keeps the paths whose key or path contains the text, with their ancestors, and the first match is selected |
| `n` / `N` | tree, data | next / previous match, wrapping |
| `t` | tree, data | type filter: string, number, bool, null, object, array, then off |
| `esc` | tree, data | clear the search and the type filter |
| `v` | tree, data | show the selected path's values as indented JSON (toggle; cut at 4 MB) |
| `y` | anywhere | copy: the path on the tree, the row or value as JSON in the data and the detail. The copy goes to the ochazuke registers (`"a y` names one) and to the system clipboard (pbcopy, wl-copy, xclip or xsel, whichever is on PATH). The status line says what was copied; a clipboard tool that fails is a footer notice, and the registers still hold the copy. `.` copies again at the focus (`y j .` copies the next row) |
| `R` | tree, data | read the file again |
| `:` | anywhere | the command palette: every verb, by name |
| `q`, `ctrl+c` | tree, data | quit |

No function keys are bound.

## Checks

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

The tests cover structure inference on mixed and optional data, column order, a 50 MB document (100,000 items; 10,000 under `-race`), the viewer through its keys, frames at 80 and 120 columns (`testdata/view-*.golden`, `UPDATE_GOLDEN=1` rewrites them), the windowed table at 100,000 rows, and `ValidateBindings`.
