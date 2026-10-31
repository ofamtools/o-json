# o-json tutorials

All 25 tutorials are authored. Each starts by opening a temporary copy of a
sample file, plays to a reset point, then teaches one action at a time from
that same reset. Captions say the key, its result and why to use it. Native
notes explain the opening and reset. No lesson depends on an earlier one.

The owner approved **select two fields in SPLIT** (`split-right-visual`)
before the other 24 were written. Its story is unchanged from that review:
mark, leave VISUAL, put first; then copy, delete, flatten, wrap, dedent and
edge behaviour. The smaller `tutorial-selection.json` and
`tutorial-reshape.json` documents make the structural changes easy to see.
Orders and sample documents supply table, search and editing examples.

Watch from the o-json worktree in a terminal at least 150 columns by 44 rows:

```sh
nice -n 15 make story-build GO_ENV='GOWORK=/tmp/ojj-tutorials/go.work GOTMPDIR=/tmp/ojj-tutorials/go-tmp GOFLAGS="-trimpath -p=2" GOMAXPROCS=3'
env -u TUIOS_ENV -u TUIOS_SESSION -u TUIOS_PANE_ID \
  HOME=/tmp/ojj-tutorials/o-json/home \
  XDG_CONFIG_HOME=/tmp/ojj-tutorials/o-json/config \
  XDG_STATE_HOME=/tmp/ojj-tutorials/o-json/state \
  XDG_DATA_HOME=/tmp/ojj-tutorials/o-json/data \
  TMPDIR=/tmp/ojj-tutorials/o-json/tmp \
  bin/o-json-story story open tutorials/split-right-visual
```

Use another stem from the table below to watch that lesson. `story run
tutorials/<stem>` checks its actions headlessly. The story fixture isolates
its documents and state; the story command has no `--data` flag. The lane's
explicit go.work uses the monogatari worktree until the coordinator tags the
library and updates the dependency. Do not commit a replace or go.work.

Press `a` for autoplay, `space` to step, `[` / `]` for previous/next action,
and `r` to restart from file opening. Pace remains 400 ms per event with a
3 second pause before and after each action. `:set play.captions false`
hides captions (default on); `:set play.action-pause 0s` removes the extra
pause. The controls apply to every app using monogatari.

Follow the JSON jobs order:

| Job | Tutorial | Actions | Lesson |
|---|---|---:|---|
| Read | [open-chooser](open-chooser.json) | 10 | choose and open a file |
| Read | [palette](palette.json) | 9 | run commands by name |
| Read | [help](help.json) | 4 | find a key in help |
| Shape | [shape-edited](shape-edited.json) | 49 | navigate the shape and viewer |
| Shape | [shape-original](shape-original.json) | 6 | read and copy the original shape |
| Shape | [empty](empty.json) | 5 | filter paths and types |
| Find | [search-line](search-line.json) | 23 | edit the search line |
| Find | [results](results.json) | 3 | walk the search results |
| Copy paths | [flat](flat.json) | 5 | copy paths and values from FLAT |
| Copy paths | [json](json.json) | 6 | read and fold formatted JSON |
| jq | [jq-structure](jq-structure.json) | 14 | build jq from the highlighted shape |
| jq | [jq-result](jq-result.json) | 1 | copy a jq answer |
| jq | [jq-panel](jq-panel.json) | 27 | read the generated jq and mapping |
| Table | [table](table.json) | 41 | read, copy and sort order rows |
| Table | [nested](nested.json) | 8 | inspect a nested customer |
| Table | [detail](detail.json) | 3 | drill into a record detail |
| Edit | [edit-data](edit-data.json) | 5 | edit values without touching the source |
| Edit | [value-editor](value-editor.json) | 4 | edit a JSON object as several lines |
| Edit | [confirm-quit](confirm-quit.json) | 2 | decide what to do with unsaved edits |
| Restructure | [edit-structure](edit-structure.json) | 11 | reshape a document in the viewer |
| Restructure | [visual-structure](visual-structure.json) | 2 | unwrap and extend a structure selection |
| Restructure | [split-left](split-left.json) | 2 | copy from the original structure |
| Restructure | [split-right](split-right.json) | 12 | build and export a restructured tree |
| Restructure | [single-left](single-left.json) | 2 | scroll a single-pane structure |
| Restructure | [split-right-visual](split-right-visual.json) | 10 | select two fields in SPLIT |

The hub tool `tools/keymap-outline/tutorials.py` assigns 264 distinct
noun.verb + literal-args identities, each taught once, across these 25
home states. Hidden rows count too. The original exact-base outline had
263 actions; selecting two real fields makes node.dedent available, giving
264 actions from 2,715 measured rows over 51 states. Stable action IDs audit
coverage while labels use words for people. Existing authored stories are
preserved by the generator; do not use `--overwrite` on this directory.

The survey includes inherited bindings that lack a target in this app:
page-surface scrolling, detail-panel scrolling and inter-region focus each
have two actions. Their lessons state that limit and demonstrate the
applicable editor or pane motion. The shared cell copier is demonstrated
by its named command in record detail because the local y shortcut calls
the app's value copier. Read-only R intentionally changes nothing, and
right-side d p intentionally explains that the original cannot be edited.
These are limits taught explicitly, not claims that the inherited keys
perform a visible scroll on these pages.

The separate split-preview lane changes the restructure page. These lessons
target a4b8e6b; the coordinator will rebase and gate them after it lands.
Proposed hidden/hint changes are recorded in the lane report, without
editing any binding rows or owner configuration.

Compared with Vim: reset replay and player action navigation have no Vim
counterpart. V, yank, put and undo keep their binding-defined app behaviour,
including VISUAL put replacing a selection. The detail's named cell-copy
command has no Vim counterpart; it exposes an inherited command whose
local shortcut is shadowed.
