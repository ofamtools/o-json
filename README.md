# o-json

A terminal workspace for JSON and JSONL, built on Ochazuke v2. The structure
tree on the left merges every array element into one shape, and it drives
navigation, projection, relational flatten, shape flips, jq and CSV export
in the data view on the right.

```
o-json                      # editable scratch buffer [No Name]
o-json FILE                 # FILE read-only (VIEW); ctrl+e or --edit to edit
o-json new.json             # a file that does not exist yet opens empty and editable
curl -s URL | o-json        # stdin, JSON or JSONL, read-only
o-json --edit FILE          # start editable
o-json --config DIR FILE    # bindings.json, themes.json, settings.json from DIR
o-json --smoke [FILE]       # scripted session without a terminal, one line per check
```

With no FILE and no piped input, o-json never errors. It opens an unnamed
scratch buffer you can type or paste into at once, and asks for a file name
only when you save. With piped stdin, keys come from the controlling
terminal (`/dev/tty`), so the interactive workspace needs one. Without a
terminal use the subcommands below.

## Workspace

The left pane is the structure tree. Each field shows its type (`string`,
`number`, `bool`, `object`, `array`, `null`, or several joined by `|`), how
often it is present, and how many items its arrays hold. A field whose type
differs between elements shows `mixed`. `j`/`k` walk it, `h`/`l` fold and
unfold, `gg` / `g G` go to the top and bottom, and `space` selects fields at
any depth for a projection or a flatten. `tab` switches between the tree and
the data view.

The right pane has four views of the focused path. Switching keeps the focus.

| key | view |
| --- | --- |
| `1` | Table: the focused array or the flattened rows. The cursor's column widens to show its full text. Filter, sort and group work on any column. |
| `2` | Nested data tree. Fold or unfold one node or the visual selection. |
| `3` | Pretty JSON or `key: value`, scrolled to the focused field. |
| `4` | Raw text with Vim editing over the whole document or the focused subtree, format/minify toggle, live validation. |

Inside the raw editor a digit is a Vim count, so the views there are
`alt+1` to `alt+4`, and `alt+4` (or `ctrl+t`) switches between the whole
text and the focused subtree. `=` cycles pretty, minified and as typed.
`esc` in normal mode or `tab` goes back to the tree. In the nested view `v`
selects rows and `h`/`l` then fold or unfold all of them.

Paths are always written from the document root: `$` is the root, then
`orders[0].memo`, `orders[].id` for every element, and `["odd key"]` for a
key that is not a plain name. `y p` copies the focused path, `y y` its value
as JSON. A step run on a nested array keeps the paths of everything around
it, so after `K` on `orders[].order_id` the result is at `orders.A1`.
While you type raw JSON, nothing blocks. A half-typed document shows
`incomplete JSON: line X, col Y` in the status line and the structure tree
keeps the last text that parsed.

## Transforms

Every transform is one step in an undoable pipeline over the document, with
a live preview. `u` undoes and `ctrl+r` redoes pipeline steps and raw edits
alike. Files open read-only, and the pipeline still works on them because it
never changes the file.

A step on a field inside a nested array (say `orders[].total`) runs on that
array only. It is recorded as a scope to the array, the step, and a scope
back to the whole document, and `u` takes all three back at once.

| key | step |
| --- | --- |
| `p` | project the fields selected with `space` |
| `F` | flatten to rows. The deepest selected array is the row grain, shallower fields repeat on every row, empty arrays keep their parent row (left join) unless `I` turns on inner join. Unrelated sibling arrays at one depth open a picker for the grain, or allow a cross product. A shallower unrelated array is left out. |
| `K` | array to object, keyed by the focused field |
| `E` | object to array of entries. A prompt asks for the key column's name, prefilled with `key` |
| `G` | group rows by the focused column. In the table a bare `g` does the same once the `g` prefix times out |
| `r` / `x` | rename / drop the focused column |
| `s` | sort by the focused column: ascending, again descending, again off |
| `/` | filter all columns: substring, fuzzy or regex (`ctrl+m` or `alt+m` cycles) |
| `f` | filter one field: `==`, `!=`, `>`, `<`, `~regex`, or contains |
| `J` | jq bar |

Many terminals send `ctrl+m` as `enter`. There `ctrl+m` accepts the filter,
and `alt+m` cycles its mode.

The jq bar shows the jq program equivalent to the pipeline plus the fields
selected on screen. Edit it to run real `jq` in the background. A newer
query cancels the one still running, `esc` cancels it too, the status shows
running, an error or ready, and `enter` opens a ready result as a new
buffer. Builder chips on `alt+f`, `alt+s`, `alt+l`, `alt+o` and `alt+g` add
steps on the focused field: `map(select(FIELD != null))`, `map({key:
FIELD})`, a `flatten`, `sort_by(FIELD)` and `group_by(FIELD)`. `y j` copies the jq command (with
`jq -s` for JSONL) to the clipboard.

## Buffers and splits

| key | action |
| --- | --- |
| `ctrl+n` | new scratch buffer |
| `ctrl+o` | open a file into a new buffer |
| `]b` / `[b`, `:bn` / `:bp` | next / previous buffer |
| `ctrl+6` | alternate buffer |
| `:b`, `g b` | pick a buffer |
| `:e PATH`, `:enew` | open a file, new scratch buffer |
| `ctrl+w v`, `:vs` | show a second buffer side by side |
| `ctrl+w w` / `ctrl+w q` / `ctrl+w o` | next window / close it / keep only this one |
| `ctrl+e` | make the buffer editable |
| `ctrl+s`, `:w [PATH]` | save |
| `W` | save as: pick a format, then a folder, then a file name |
| `t` / `d` (also `g t` / `g d`) | next theme / next density |
| `q`, `:q`, `:q!` | quit (`q` asks again while a buffer has unsaved edits) |

`ctrl+s` picks the format from the extension (`.csv`, `.tsv`, `.jsonl` or
`.ndjson`), else JSON. `W` always offers pretty, minified, JSONL, CSV and
TSV, then the folder picker (`ctrl+s` there takes the folder shown), then a
file name with the matching extension. The picked format wins over the
extension. With the table view showing, CSV is preselected and CSV or TSV
writes exactly the table on screen. Elsewhere CSV and TSV write the rows of
the whole result.

- A document saved as JSON with no pipeline is written as you typed it.
- A pipeline result, CSV or TSV is an export, and the buffer keeps its file
  and its unsaved state.
- An editable buffer saving its document as JSON takes the new name and is
  clean again.
- Text that does not parse is never saved.
- A read-only buffer never overwrites the file it came from, through a
  symlink or hard link either. Save it under another name or press `ctrl+e`.
- Every write goes to a temporary file beside the target and is renamed into
  place, keeping the file's mode and writing through symlinks.

Press `?` for the keys active where you are and `:` for every command.

## Commands without a TTY

Each reads FILE, or stdin as `-`, and prints only on success. Errors go to
stderr, with exit status 1 when the input cannot be read and 2 when the
request cannot run.

```
o-json structure FILE|- [--format text|json]
o-json flatten FILE|- [--select PATH,PATH] [--field PATH]... [--grain PATH] [--inner] [--cross] [--format csv|json|table|tsv|jsonl] [--jq]
o-json query FILE|- [--compact] [--] EXPR
```

`structure` prints the merged shape, one line per field:

```
$               object  1/1
  items         array   1/1  4 items
    []          object    4
      owner     object  4/4
        email   mixed   2/4  optional
```

`flatten` writes relational rows (CSV by default). Paths use `orders[].id`
or `$[].orders[].id`. `--select` takes a comma-separated list, and commas
inside a quoted key stay part of it (`--select '["x,y"],orders[].id'`).
`--field` adds one path per flag. With neither, every field is included.
`--format table` prints aligned columns in the relation's column order, with
tabs, newlines and other control characters in cells shown as escapes. When two
sibling arrays could each be the rows it stops and names them, so you pick
one with `--grain` or allow `--cross`. A deeper array wins over a
shallower unrelated one, whose column is left out. `--inner` drops parents whose array
is empty. `--jq` prints the equivalent jq program instead of the rows.

`query` runs `jq -c -- EXPR` over the input and prints each result as pretty
JSON (one line each with `--compact`). An expression that starts with a dash,
such as `-.a`, is taken as the expression unless it names a flag of the
command. Put `--` before it to be sure. No results print nothing. jq's
stderr appears only when jq fails, and ctrl+c stops it with status 130.

## Requirements and dependencies

- Go 1.26.
- `jq` on `PATH` for the jq bar and `query` (Homebrew: `brew install jq`).
  Everything else works without it.
- Imports are the standard library, `charm.land/bubbletea/v2` and
  `ofam.dev/ochazuke/...` (v2). `go.mod` replaces `ofam.dev/ochazuke` with
  the local `../ochazuke-next` checkout until Ochazuke v2 is tagged. v1's
  `oden`, gojq and Ochazuke v1 packages are gone.

## Configuration

Settings, bindings and themes are optional JSONC files in
`$XDG_CONFIG_HOME/o-json-next` (else `~/.config/o-json-next`, or the folder
given with `--config DIR`). Without them the library defaults apply. o-json
never writes them, and a broken file shows as a note in the status line.

- `settings.json`: `theme` (a preset name such as `dark` or `light`),
  `density` (`compact`, `balanced`, `spacious`), `borders` (`rounded`,
  `sharp`, `minimal`, `none`), `matchMode` (`substring`, `fuzzy`, `regex`),
  `clipboard` (`false` keeps yanks in the register) and `innerJoin`.
- `bindings.json`: Ochazuke bindings merged over the defaults, for example
  `[{"mode": "normal", "keys": ["ctrl+g"], "command": "shape.group"}]`.
  The command names are the ones `?` and `:` list.
- `themes.json`: an Ochazuke theme laid over the chosen preset.

## Known limitations

- Raw-view jumps look up the displayed path in the source text, so after a
  reshaping pipeline the cursor lands on the deepest span that still matches.
- In the tree pane a jq chip reads a field key holding a dot as a nested path.
- Each scoped step re-runs the pipeline for every part it adds while it is
  checked, which is noticeable only on very large documents.
- The interactive workspace needs a terminal. The subcommands do not.
