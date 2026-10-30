# o-json tutorials

One tutorial per home state, with every distinct bound action taught once.
Hidden bindings are included. `setup` reaches a reset point from opening a
file; each action starts fresh from that point. Notes and captions are native
monogatari fields.

The first authored tutorial is **select two fields in SPLIT**
(`split-right-visual.json`). Open the file, select first/last in the right
structure, then learn mark, leave VISUAL, put, copy, delete, flatten, wrap,
dedent and edge behaviour. Every action has a light expect. It reads the
small `testdata/tutorial-selection.json` fixture; the real file, recent list
and state directory used in playback are temporary.

```sh
make story-build GO_ENV='GOWORK=/tmp/ojj-tutorials/go.work GOTMPDIR=/tmp/ojj-tutorials/go-tmp GOFLAGS="-trimpath -p=2" GOMAXPROCS=3'
# Use a temporary HOME/XDG environment as in /tmp/ojj-tutorials/review.md:
bin/o-json-story story open tutorials/split-right-visual
bin/o-json-story story run tutorials/split-right-visual
```

The lane go.work uses the monogatari worktree until its new format is tagged.
The Makefile's usual `GOWORK=off` build will use that tag after the dependency
bump. Do not commit a local replace.

In the player, `a` starts autoplay (400 ms per event, 3 seconds before/after
actions), `space` steps, `[` / `]` reset to previous/next action, and `r`
restarts from opening. `:set play.captions false` hides narration; the setting
defaults on. `:set play.action-pause 0s` removes the extra action pause.

The remaining 24 files are **generated skeletons**, intentionally skipped
until the owner reviews the first tutorial. They are not completed lessons.
The hub's `tools/keymap-outline/tutorials.py` generated the inventory:
264 actions in 25 home states, 2,715 measured rows over 51 states. The original
263-action survey selected only the empty SPLIT root; selecting two fields
also makes `node.dedent` available. Stable action IDs keep coverage auditable
while labels use words for people. Do not overwrite authored files when
regenerating; the generator preserves them by default.

The first three lessons follow the owner's order. Put swaps the two selected
name fields for the saved city field in the right structure; both origins are
inside the same customer array, so the mapping remains valid. Each action
resets, so the later lessons still begin with first/last selected.

The separate split-preview lane changes this page. This tutorial targets
a4b8e6b; the coordinator rebases it after that lane lands. Footer visibility
changes (including `d p` while reshaping) will be proposed after the remaining
tutorials are authored, without editing bindings here.

Compared with Vim: the player reset and action navigation have no Vim help
counterpart. The app's `V`, yank, put and undo keep their binding-defined
behaviour, including VISUAL put replacing the selection.
