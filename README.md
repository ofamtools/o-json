# o-json

A JSON structure and table viewer for the o family, built on [ochazuke](../ochazuke) (Bubble Tea v2) with an [oden](../oden) command line. It answers two questions about a document: what shape is it (every path, its types, how many values have it, which fields are optional), and what does an array of objects look like as a table. It never writes the document it reads unless asked: `restructure --out`, the viewer's `w` and the restructure page's write make a new file, and replace an existing one only when asked (`--overwrite`, or `W` on the page, which confirms before it writes the source).

```sh
o-json data.json                        # open the viewer (also: o-json view data.json)
o-json                                  # an empty document: e opens a file, p pastes JSON
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

`--mapping` prints the mapping with every shape it leaves out added as a drop (`{"from": ".items[].archived", "to": null}`), so it reads as the whole story of the source; the output is itself a mapping, one entry per line.

A key an object repeats is read as jq reads it: the last value, at the first one's position. The Go result, the jq program, the viewer and `table` therefore agree.

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

The viewer has a breadcrumb line and then two panels. The breadcrumb shows the file (followed by `[+]`, as in vim, while the structure has edits no write saved), the selected path with its types and presence (and `← from` for a node that moved), the selected row's own path when the data panel holds the focus, and the active filters.

- **Left: the structure, edited in place.** The structure pane is the restructure editor: it opens as a copy of the document's shape (`Structure · edited`), every shape a node mapped to itself, so the output is the input. Every key of the restructure page's tree works here (below): `d d` / `y y` / `p` / `P`, `> >` / `g >` / `> j` / `> k` / `< <`, `S`, `t`, `r`, `o` / `O` / `a`, `V` selections, `u` / `ctrl+r`. Each node keeps the shape path it came from (the `from` column), wherever it moves. Nothing is written until `w`: `w` (`restructure.write {path}`) writes the restructured document to a new file, an existing one asks first, and the source is never written; `W` replaces an existing file, and the source after one confirm. A shape whose values mix types (an object in some items, `null` in others) is one node that copies its values, so the copy never changes them; an object with more than 200 keys likewise.
- **`T` flips original and edited** (`restructure.show`): the structure pane shows the original structure tree (`Structure · original`), read-only, one row per path with its types and count (an optional field's name ends in `?`), and the data pane the original document; `T` again goes back to the edited one, on the same shape. The search and the type filter (`/`, `n`, `N`, `t`, `esc`) and `p` paste work on the original; on the edited tree `/` filters its rows, `t` turns a key into an array and `p` puts nodes. A structure with more than 150 paths opens collapsed below the second level, and an object with more than 200 distinct keys (a map) lists the first 200 and a `… N more fields` row.
- **The setting `structure.editable`** (default on) off keeps the viewer read-only, as it was: the original tree only, no `T`, and the restructure page (`M`) alone edits. It takes effect on the next document.
- **Right: the selected path's data**, from the edited document while the edited structure is shown: as a node moves, the data moves with it (the restructured document, as `w` would write it), and the focused node shows its restructured value. One array, or the values of a `[]` path, is a table with a `#` column, columns in frequency order and optional ones last. One object is labeled `field  type  value` rows, and one scalar is shown as is. Column widths come from the first 2,000 rows, so they hold still while you scroll. On a narrow screen the widest columns narrow first, and after that columns drop from the right, so the optional columns go first. Only the rows on screen are in the table, so a 100,000-row array costs one screen per key.
- **Detail**: `enter` on a table row, or on a field that holds an object or an array, opens it in a modal as labeled rows. `enter` (or `l`) on a nested object or array steps into it, and `h` or `backspace` steps back out.

The file is read off the event loop (`viewer.open`, and `viewer.load` reads it again), and the screen says `loading …` until it is done. A parse error gives the line and column. The panels sit side by side from 60 columns up and stack below that. `>` and `<` move the divider (the tree's width, or its height when stacked) and `=` puts it back; the divider is remembered in `~/.local/state/o-json/panes.json`.

### Files and the empty document

The viewer holds one document, like a vim buffer.

- **No file:** `o-json` with no file (and stdin a terminal) opens an empty document; the breadcrumb reads `[no file]` and names the keys that fill it. `: viewer new` empties the viewer from inside; when the structure has edits no write or export-map saved, it asks first (`viewer.discard`, and `u` brings the edits back).
- **Open:** `e` (`viewer.open {path}`) asks for the path in the args form. The field is free text with suggestions (`ctrl+space`, or `enter` in normal mode, opens them): the recently opened files, newest first, then the files in the current file's folder (the working directory for a document of no file). The suggestions are a list, not completion as you type: the form has no completion source that sees the typed text yet. `R` (`viewer.load`) reads the current file again. With edits no write saved, `e` and `R` ask once first (`viewer.discard {path}`); the new document starts as a copy of its shape, and `u` brings the dropped edits back.
- **Recent files:** every file opened is recorded, newest first, in `~/.local/state/o-json/recent.json` (`$XDG_STATE_HOME` if set). The setting `recent.size` (default 10; 0 keeps none) is how many; a file that is gone is left out. `: viewer recent` lists them.
- **Paste:** `p` (`viewer.paste`) parses the system clipboard (pbpaste, wl-paste, xclip or xsel) into the buffer, a document of no file. Invalid JSON is an error naming its line and column, and the buffer stays as it was. `viewer.paste {text}` pastes the text given (the palette, a script).
- **Write:** in the edited view `w` writes the restructured document (above). On the original, `w` (`viewer.write {path}`, args form) writes the shown document as indented JSON to a new file; an existing file asks first (`viewer.overwrite`) and the source is never written. A document of no file is then that file. The restructure page works on a pasted document as on a file.

### Keys

`?` lists every key the focus answers to, generated from the binding rows. The footer shows the keys that are not motions.

| Key | Where | Does |
|---|---|---|
| `j` `k`, arrows | anywhere | move (counts work: `5j`) |
| `l` / `h` | tree | expand, or step onto the first child / collapse, or step onto the parent |
| `enter` | tree | expand or collapse |
| `tab` / `shift+tab` | anywhere | the other panel |
| `>` / `<`, `=`; `ctrl+w >` / `ctrl+w <`, `ctrl+w =` | tree, data | grow / shrink the tree panel (counts work: `3>`, `3 ctrl+w >`), reset the divider |
| `g g` / `G`, `home` / `end` | data, detail | first / last row (`12G` goes to row 12) |
| `ctrl+d` `ctrl+u` / `ctrl+f` `ctrl+b`, `pgdown` `pgup` | data, detail | half a screen / a screen |
| `enter` | data | open the row's item (or the field's object or array) in the detail |
| `h` `l`, `left` `right`, `0` `^` / `$`, `N\|`, `g c` | data, detail | move the current cell's column: one left / right (in the detail `h` and `l` are the entry's), first / last, column N, choose one |
| `z >` / `z <`, `z =`, `z p` | data, detail | widen / narrow the current column, make every column automatic, pin the current column |
| `enter` `l` / `h` `backspace` | detail | step into the nested value / back out (at the top, close) |
| `q`, `esc` | detail | close |
| `T` | tree, data | flip the structure and data panes between the edited and the original document (`restructure.show`) |
| `/` | tree, data (original) | search the paths: the tree keeps the paths whose key or path contains the text, with their ancestors, and the first match is selected |
| `n` / `N` | tree, data (original) | next / previous match, wrapping |
| `t` | tree, data (original) | type filter: string, number, bool, null, object, array, then off |
| `esc` | tree, data (original) | clear the search and the type filter |
| `v` | tree, data | show the selected path's values as indented JSON (toggle; cut at 4 MB) |
| `y` | anywhere | copy: the path on the tree, the row or value as JSON in the data and the detail. The copy goes to the ochazuke registers (`"a y` names one) and to the system clipboard (pbcopy, wl-copy, xclip or xsel, whichever is on PATH). The status line says what was copied; a clipboard tool that fails is a footer notice, and the registers still hold the copy. `.` copies again at the focus (`y j .` copies the next row) |
| `R` | tree, data | read the file again |
| `e` | tree, data | open a file (args form; suggestions: recent files, then the current folder) |
| `p` | tree, data (original) | paste JSON from the clipboard into the buffer (on the edited tree `p` puts nodes) |
| `w` / `W` | tree, data (edited) | write the restructured document to a new file / replacing a file, the source after one confirm |
| `w` | tree, data (original) | write the shown document to a new file |
| node keys | tree (edited) | as on the restructure page's restructured tree (below) |
| `s` | tree (original) | send the shape to the restructure (below) |
| `M` | tree, data | open the restructure page |
| `:` | anywhere | the command palette: every verb, by name |
| `q`, `ctrl+c` | tree, data | quit |

No function keys are bound.

## The restructure page

`M` opens a page for building a restructure mapping by hand, and `q` goes back to the viewer. It opens focused on the original structure, on the shape the viewer has selected, every time it opens (in the single layout, showing the original). It works on shapes, as `restructure` does: one node maps that field of every item, and an optional field maps only where it is present. The page's state *is* a mapping: the Mapping panel shows it live in the `--mapping` form (or why it does not check), and `o-json restructure FILE --map` runs it the same way.

- **The document is the first node.** The restructured tree always starts with `. (root)`, the new document itself, an object (or, after `t`, an array). It is an ordinary node with no key: `d d`, `r`, `< <`, `> >` and `S` do not apply to it, so those keys do nothing, and nothing goes beside it (`o` on it adds its first key, `O` is refused).
- **One state with the viewer.** The restructured tree is the viewer's edited structure, drawn here beside the original: the same nodes, undo stack and mapping, so an edit on either shows on both. With edits pending, `M` opens on them.
- **It starts empty.** With no edits, `M` empties the copy as one undo step (`u` brings it back), and going back to the viewer with nothing sent gives the viewer its copy again. `s` on a shape of the original (or in the viewer's original structure tree) sends it across, with the ancestors it needs (`.items[].owner.name` brings `items` and `[]`) and its fields. A node that came from the same shape is reused, wherever it has moved. `V` selects several shapes, and `s` sends them all as one change.
- **Every node keeps its origin.** The `from` column is the shape a node came from. Renames and moves change only where it goes (`to`), and the breadcrumb shows `to ← from`.
- **Editing** (on the restructured tree), with vim's keys. `> >` nests the node into the nearest sibling above that takes keys (an object, or a wrapper), as its last key, and `g >` into the nearest below, as its first; plain siblings between are skipped (`node.indent {into: previous|next}`). `> j` and `> k` nest this node and the one below or above, as vim's `>j` takes two lines (`node.indent {with: next|previous}`). `< <` moves the node, with its children, out to its parent's level just before the parent; dedenting a parent's children one by one stacks them before it in order, and the parent stays, an empty object once nothing is left in it (its `from` still shows where it came from). `< <` out of an array item collects the values: `.items[].id` becomes `.id[]`, an array of every id, placed just before the array's key; it does not apply on the document's own item, to a wrapper (nothing to collect) or to a key of the root. `S` wraps the node in a new object in its place (`node.wrap`). Each applies only when the result still maps; where it does not, the keys do nothing. Alone, `>` and `<` are prefixes (the badge reads `NORMAL >`) and do nothing, so on this tree they never resize the divider: `ctrl+w >`, `ctrl+w <` and `ctrl+w =` do, on every page (counts work), and `>` `<` `=` still do on the original tree and in the viewer. `V` marks rows (in normal mode), and `> >`, `< <` act on every marked node as one undo step. `r` edits the key in place, `J`/`K` move a node among its siblings, and `u`/`ctrl+r` undo and redo from either side.
- **Yank, delete, put** (on the restructured tree), through ochazuke's registers. `y y` (`node.yank`) yanks a node with its children, `d d` (`node.delete`) deletes it into the register, and `"a y y` / `"a d d` use register `a`. `p` / `P` (`node.put {before}`) put the register's nodes at the level where they land: on a plain key, `p` puts the next sibling and `P` the previous one; on an expanded object or array, `p` puts its first child and `P` its previous sibling; on a collapsed one, `p` puts the next sibling after the whole object and `P` the previous one; on `. (root)`, `p` and `P` both put its first key. A delete then a put is a **move**: the node comes back as it was, with its `from` and its children. A yank then a put is a **copy**: the same shape mapped again. A key that lands beside a sibling of the same name is numbered (`a-2`, `a-3`), as the placeholder is, since two siblings with one key would not check. A put whose mapping would not check (a key into an array item, or a field from outside the item it lands in) is refused with a notice and changes nothing. Over a `V` selection, `d` and `y` (`restructure.delete`, `restructure.yank`) delete or yank every selected node, as one undo step; `d d` and `y y` are not available then. Every put and delete is one undo step.
- **New keys open selected.** A key the page makes (the wrapper `S` makes, and `o`/`O`/`a`, `restructure.add-key {where}`, an empty object below, above or inside the node) gets a placeholder, `new-key`, opened in select mode: typing replaces it, and `esc` or a motion keeps it. The placeholder is the setting `restructure.new-key`, numbered past its siblings' keys (`new-key-2`, `new-key-3`). A wrapper has no `from` in the mapping. Making the key is one undo step and the typed name another, so `u` first takes the name back and then the key. With `{key}` (from the palette or the CLI) the key is named at once.
- **Arrays and objects**: `t` (`node.array`) turns a key into an array of itself, its keys or its value becoming the array's `[]` item (`.items[].owner` becomes `.items[].owner[]`, one owner per array). `t` on that array or its item (`node.object`) turns it back. Each applies only where the mapping still checks, so on `.items[]`, whose values are many, `t` does nothing: a key holds one value, and an array whose item fans out has no one value to give it (`?` leaves `t` out there, as it leaves out every key that would not answer).
- **An array document**: `t` on the empty root makes the top level an array, `. (root) array`, whose `[]` item waits for a shape. The first shape sent fills it with the array it is in: `s` on `.items[]` (or on `.items[].id`) maps `.items[]` to `.[]`, so the output is the array of items (`jq '[.items[] | …]'`). A root holding one array key turns into that array with `t` (the key goes, its item moves up); a root holding other keys becomes an array of itself, `[ {keys} ]` (`{"from": ".", "to": ".[]"}`). `t` on an array root turns it back: under the key it came from (`.[]` from `.items[]` becomes `.items[]`), back to the document's keys, or, for an array document's own array, under one new key, its placeholder opened selected (`{"new-key": [...]}`). Each flip is one undo step, the cursor on the toggled node.
- **Origins light up.** Selecting a node on the restructured tree marks the shape it comes from on the original (the accent colour), and selecting a shape on the original marks the nodes it went to.
- **Layouts**, setting `restructure.layout` (`L` on the page, `o-json settings set restructure.layout single`, or `: restructure layout`): `split` (default) puts the original on the left and the restructured tree with its mapping on the right, and `tab` moves between them. `single` shows one tree above the mapping, and `tab` swaps the original and the restructured tree. `L` keeps the focused tree, and the breadcrumb names the layout and the key (`split layout (L: single)`).
- **Output**: `w` (`restructure.write {path}`) writes the restructured document to a new file, asking for the path in a form (for example `NAME.restructured.json`). An existing file asks before it is replaced (`restructure.overwrite`), and the source is never written. `W` (`restructure.write {path, force: true}`) replaces an existing file without asking and, after one confirm (`restructure.overwrite-source`: "Overwrite the source people.json …?"), the source too, as `--out SOURCE --overwrite` does on the command line. `: restructure jq` copies the jq program, and `: restructure export-map {path}` writes the mapping to a new file.

| Key | Where | Does |
|---|---|---|
| `s` | original | send the shape (and the ancestors it needs, and its fields) |
| `> >` / `g >` | restructured | nest into the object above (as its last key) / below (as its first) |
| `> j` / `> k` | restructured | nest this node and the one below / above |
| `< <` | restructured | move out, just before the parent (out of an array item: collect into an array) |
| `S` | restructured | wrap in a new key, its placeholder selected |
| `>`, `<` | restructured | prefixes only: alone they do nothing |
| `ctrl+w >` / `ctrl+w <`, `ctrl+w =` | page | grow / shrink the original's panel (counts work), reset the divider |
| `o` / `O` / `a` | restructured | a new key below / above / inside, its placeholder selected |
| `t` | restructured | a key to an array of itself, and back; on the root, the document object ↔ array |
| `r` | restructured | rename the key |
| `d d` / `y y` | restructured | delete into the register / yank (`"a` first: register a) |
| `p` / `P` | restructured | put below / above, at the level where it lands (on the root: its first key) |
| `V` … `d` / `V` … `y` | restructured | delete / yank every selected node |
| `u` / `ctrl+r` | page | undo / redo |
| `tab` | page | the other tree (split: focus; single: show) |
| `L` | page | flip the layout (split / single), keeping the focused tree |
| `w` | page | write the restructured document to a new file (an existing file asks; never the source) |
| `W` | page | write, replacing an existing file without asking; the source after one confirm |
| `q` | page | back to the viewer |

## Checks

```sh
gofmt -l . && go vet ./... && GOWORK=off go test -race ./...
go run . --smoke testdata/sample.json
go run . catalog >/dev/null
```

The tests cover structure inference on mixed and optional data, column order, a 50 MB document (100,000 items; 10,000 under `-race`), the viewer through its keys, the empty document, open, paste, new and the recent list, frames at 80 and 120 columns (`testdata/view-*.golden`, `UPDATE_GOLDEN=1` rewrites them), the windowed table at 100,000 rows, and `ValidateBindings`.
