# o-json tutorial plan

High-level user goals and step-by-step action tables for `o-json`, verified against `o-json v1.2.0` default bindings.

## High-level user goals

1. **Explore deep nested JSON from the structure pane.** Open a document, jump to the bottom of the structure tree with `G`, walk into level-3 nested arrays like `orders[].items[]`, and inspect all elements collected across every parent in Table, Nested, and Pretty views.
2. **Mark fields across depths and flatten into a clean table.** Select fields at different nesting depths with `Space` and press `F` to build a flat relational table that preserves original leaf key names.
3. **Reshape nested trees with hoist, splat, nest, and convert.** Lift a deep field up one level or splat an entire object into its parent with `<`, move fields into an existing sibling object or a new child object with `>`, and convert object shapes with `C`.
4. **Move fields side by side across split windows.** Transfer focused or marked fields into a side-by-side target buffer with `M`, switch windows with `ctrl+w w`, and send fields back or undo cleanly with `u`.
5. **Filter, sort, and group records from either pane.** Run a global search with `/`, stack a numeric field predicate with `f`, sort ascending and descending with `s`, and group rows into buckets with `g b`.
6. **Build live `jq` queries with chips and the stage picker.** Open the live `jq` bar with `J`, open the interactive stage picker with `Tab`, append builder chips like `alt+c`, materialize results into a new buffer with `Enter`, and switch back with `ctrl+6`.
7. **Fold raw JSON, batch-unfold visually, and yank values or paths.** Fold closures in Raw view with `z M`, `z R`, and `z a`, batch-unfold rows with `v` and `l` in Nested view, copy exact values and paths with `y y` and `y p`, and open `?` or `:`.

---

## 1. Explore deep nested JSON from the structure pane

Goal: drive deep inspection from the left structure pane and preview every element of a nested array across all parent records.

| Action | Keys | What it does and why |
| --- | --- | --- |
| Open file | `o-json orders.json` | Opens the file read-only with the merged structure tree on the left and synchronized data on the right. |
| `motion.bottom` | `G` | Jumps straight to the `orders[]` element node at the bottom of the structure tree. |
| `motion.right` | `l` | Expands `orders[]` to reveal its nested fields, objects, and arrays. |
| `motion.down` | `j j j j j j` | Moves the structure cursor onto the nested `items` array inside `orders[]`, immediately collecting all 8 line items across all 6 orders in the right pane. |
| `view.table` | `1` | Switches the right pane to Table view so all 8 line items from every order render together as rows. |
| `view.pretty` | `3` | Switches to Pretty JSON view showing the collected array of all 8 line items across every order. |
| `motion.top`, `motion.bottom` | `g g` then `G` | Returns to the root and jumps back to the bottom of the structure tree with standard Vim motions. |

---

## 2. Mark fields across depths and flatten into a clean table

Goal: mark fields from different nesting levels in the structure tree and flatten them into a denormalized table that keeps original leaf key names.

| Action | Keys | What it does and why |
| --- | --- | --- |
| `select.toggle` | `Space` | Marks `orders[].order_id` at depth 2 in the structure tree. |
| Expand `customer` and mark `email` | `j j j j l j j j Space` | Walks into `orders[].customer` and marks `email` at depth 3. |
| Expand `items[]` and mark `sku` and `qty` | `j j l j l j Space j j Space` | Walks into `orders[].items[]` and marks `sku` and `qty` at depth 4. |
| `flatten` | `F` | Flattens the marked fields at the deepest array grain and opens Table view with original leaf columns `order_id`, `email`, `sku`, and `qty`. |
| `history.undo` | `u` | Undoes the flatten step in one keystroke and restores the original nested schema. |

---

## 3. Reshape nested trees with hoist, splat, nest, and convert

Goal: restructure nested objects directly from the structure tree with undoable steps that synthesize clean `jq`.

| Action | Keys | What it does and why |
| --- | --- | --- |
| Focus level-4 field `city` | `l j` | Expands `orders[].customer.address` and focuses `city` at level 4. |
| `shape.hoist` | `<` | Lifts `city` up one level from `address` directly into `customer` across every order. |
| Move to `address` and `shape.hoist` | `k <` | Splats the remaining keys of `address` directly into `customer` and removes the empty wrapper object. |
| Focus `priority` and `shape.nest` | `g g G` then navigate to `priority` and press `>` | Opens the Nest picker listing existing sibling object `customer` plus the option to create a new nested object. |
| Confirm sibling object | `Enter` | Moves `priority` directly into the existing `customer` object across every order in one keystroke. |
| `history.undo` | `u u u` | Reverts the nest, splat, and hoist steps in reverse order. |
| Focus `store` and `shape.convert` | `g g j C` | Opens the Convert Shape picker on `store` offering Entries array, Values array, and Keys array. |
| Confirm Entries array | `Enter` then `Enter` | Converts `store` into a key-value entries array, then `u` restores the original object. |

---

## 4. Move fields side by side across split windows

Goal: build a clean target JSON document in a side-by-side split buffer by transferring focused or marked fields across windows.

| Action | Keys | What it does and why |
| --- | --- | --- |
| Focus `store` and `shape.transfer` | `g g j M` | Opens a side-by-side split with a new `reshaped.json` scratch buffer on the right and copies `store` into it. |
| Mark fields and `shape.transfer` | Navigate to `order_id`, press `Space`, then `M` | Copies the marked field across into `reshaped.json`, merging it cleanly alongside `store`. |
| `window.next` | `ctrl+w w` | Switches focus into the right-hand `reshaped.json` window. |
| `history.undo` | `u` | Undoes the last transfer inside `reshaped.json` without affecting the source file on the left. |
| `window.only` | `ctrl+w o` | Closes the extra split window and returns to the main workspace. |

---

## 5. Filter, sort, and group records from either pane

Goal: narrow rows with global and column filters, stack multi-key sorts, and group records with `g b`.

| Action | Keys | What it does and why |
| --- | --- | --- |
| Focus `orders` in Table view | `1` then `Tab` | Opens `orders` in Table view and focuses the table columns. |
| `filter.all` | `/` then `shipped` then `Enter` | Filters across all columns to keep only shipped orders. |
| `filter.field` | `l l f` then `>100` then `Enter` | Stacks a numeric filter on `amount` to keep only shipped orders above 100. |
| `history.undo` | `u u` | Pops both filters to restore all six orders. |
| `sort.cycle` | `s` then `s` | Sorts by `amount` ascending, then flips to descending so the largest order is first. |
| `shape.group` | `h g b` | Moves to `status` and groups rows into `cancelled`, `pending`, and `shipped` buckets. |
| `history.undo` | `u u` | Undoes the grouping and sort steps cleanly. |

---

## 6. Build live `jq` queries with chips and the stage picker

Goal: compose a live-previewed `jq` query using the interactive stage picker and shortcut chips, then open the result in its own buffer.

| Action | Keys | What it does and why |
| --- | --- | --- |
| `jq.open` | `J` | Opens the live `jq` prompt with the current pipeline and displays the 8 builder chips underneath. |
| Type filter prefix | ` \| .orders` | Narrows the live preview to the orders array as you type. |
| Open stage picker | `Tab` | Opens the interactive `jq` Builder picker listing each stage description and previewed snippet. |
| Pick `count/length` stage | Type `count` then `Enter` | Appends ` \| length` to the query and previews the live count of 6 orders. |
| Confirm `jq` result | `Enter` | Opens the `jq` output in a new editable buffer. |
| `buffer.alt` or `buffer.pick` | `ctrl+6` or `g B` | Returns straight to `orders.json` or opens the buffer picker with `g B`. |

---

## 7. Fold raw JSON, batch-unfold visually, and yank values or paths

Goal: fold raw JSON closures, batch-unfold nested rows with visual selection, yank values or paths, and inspect active commands.

| Action | Keys | What it does and why |
| --- | --- | --- |
| `view.raw`, `pane.toggle` | `4` then `Tab` | Opens Raw JSON view and focuses the Vim-style source pane. |
| `raw.fold.all` | `z M` | Folds every nested JSON block into a compact outline. |
| `raw.fold.none` | `z R` | Unfolds all JSON blocks back to full text. |
| `motion.down`, `raw.fold.toggle` | `j` then `z a` | Toggles folding on just the enclosing `store` object while leaving `orders` open. |
| `raw.fold.none`, `view.nested` | `z R`, `Tab`, `2`, `Tab` | Unfolds everything and switches to Nested view. |
| `nested.visual`, `motion.right` | `g g j j j h v j j l` | Selects three order rows in Visual mode and batch-unfolds all three with `l`. |
| `yank.value`, `yank.path` | `y y` then `y p` | Copies the focused JSON value and its exact JSON path to the clipboard. |
| `help.open`, `palette.open` | `?`, `Esc`, `:` | Opens context-aware key help and the searchable command palette. |
