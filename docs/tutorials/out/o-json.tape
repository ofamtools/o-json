Output o-json.mp4

Set Shell "bash"
Set Width 1280
Set Height 720
Set FontSize 15
Set Padding 14
Set TypingSpeed 40ms

Hide
Type "o-json docs/tutorials/fixtures/orders.json"
Enter
Sleep 1500ms
Show

# Section: Open & Navigate Nested JSON
# Open orders.json to inspect the merged structure tree on the left and nested data on the right.
Sleep 1400ms

# Jump to the orders element node and unfold its schema to inspect nested, mixed, and optional fields.
Type "g"
Type "G"
Type "l"
Sleep 1300ms

# Switch the right pane to Table view to display every order as a structured row.
Type "1"
Sleep 1300ms

# Jump into the table pane and move to the last column so long notes expand automatically under the cursor.
Tab
Type "$"
Sleep 1400ms

# Switch to Pretty JSON view to inspect the focused record with syntax structure preserved.
Type "3"
Sleep 1300ms

# Section: Search & Multi-Condition Filtering
# Return to Table view and jump back to the first column.
Type "1"
Type "0"
Sleep 900ms

# Filter across all columns with / to keep only shipped orders.
Type "/"
Type "shipped"
Enter
Sleep 1400ms

# Move onto the amount column before adding a field-level predicate.
Type "l"
Type "l"
Sleep 700ms

# Stack a field filter on amount (>100) to narrow shipped orders to high-value purchases.
Type "f"
Type ">100"
Enter
Sleep 1400ms

# Undo both filter steps cleanly to restore all six orders.
Type "u"
Type "u"
Sleep 1200ms

# Section: Multi-Key Sorting & Grouping
# Sort orders by amount ascending so the smallest order moves to the top.
Type "s"
Sleep 1300ms

# Press s again on amount to flip the sort order to descending.
Type "s"
Sleep 1300ms

# Move left to the status column for multi-key sorting.
Type "h"
Sleep 700ms

# Stack a sort by status while preserving amount descending within each status group.
Type "s"
Sleep 1300ms

# Group orders by status to aggregate matching records into nested row buckets.
Type "G"
Sleep 1400ms

# Undo the grouping and sort steps to return to the original dataset.
Type "u"
Type "u"
Type "u"
Sleep 800ms

# Section: Live jq Expression Bar
# Open the live jq bar and reshape high-value orders into compact summary objects.
Type "J"
Type " | .orders | map(select(.amount > 100) | {id: .order_id, total: .amount})"
Sleep 1600ms

# Press Enter to open the live jq result in a new editable buffer.
Enter
Sleep 1400ms

# Switch back to the original orders.json buffer with [ b.
Type "["
Type "b"
Sleep 1200ms

# Section: Shape Conversion, Relational Flatten & Raw JSON Folding
# Jump back to the structure tree and focus the store object.
Tab
Type "g"
Type "g"
Type "j"
Sleep 700ms

# Open the Convert Shape picker with C to inspect structural transformations for store.
Type "C"
Sleep 1400ms

# Convert store into an entries array so keys and values appear as table columns.
Enter
Enter
Sleep 1400ms

# Undo the conversion and focus the document root before flattening.
Type "u"
Type "g"
Type "g"
Sleep 700ms

# Press F to flatten nested orders and choose the row grain when sibling arrays exist.
Type "F"
Sleep 1400ms

# Confirm orders[].items[] as the row grain to flatten line items into a relational table.
Enter
Sleep 1500ms

# Undo flatten before switching to Raw JSON view.
Type "u"
Sleep 700ms

# Open Raw JSON view and focus the Vim editor pane to inspect the source text.
Type "4"
Tab
Sleep 1300ms

# Fold all nested JSON closures at once with zM to get a compact top-level outline.
Type "z"
Type "M"
Sleep 1400ms

# Unfold every JSON closure back to full text with zR.
Type "z"
Type "R"
Sleep 1200ms

# Move the raw editor cursor onto the store block.
Type "j"
Sleep 600ms

# Toggle folding on just the enclosing store object with za while keeping orders expanded.
Type "z"
Type "a"
Sleep 1400ms

# Unfold store and return focus to the structure tree.
Type "z"
Type "R"
Tab
Sleep 700ms

# Section: Visual Selection, Yanking & Discovery
# Switch to Nested view, focus the data pane, and expand the orders list.
Type "2"
Tab
Type "g"
Type "g"
Type "j"
Type "j"
Type "l"
Type "j"
Sleep 900ms

# Start visual selection with v and extend across three order nodes.
Type "v"
Type "j"
Type "j"
Sleep 1300ms

# Press l to unfold all three visually selected orders in one action.
Type "l"
Sleep 1400ms

# Yank the focused JSON value directly to the clipboard with y y.
Type "y"
Type "y"
Sleep 1300ms

# Yank the exact JSON path of the focused node with y p.
Type "y"
Type "p"
Sleep 1300ms

# Open context-aware key help with ? to inspect every binding active here.
Type "?"
Sleep 1400ms

# Dismiss key help before opening the command palette.
Escape
Sleep 600ms

# Open the : command palette to search every action and Vim-style ex command.
Type ":"
Sleep 1400ms

