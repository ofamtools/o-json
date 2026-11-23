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

# Section: Explore Deep Nested JSON from the Structure Pane
# Open orders.json to inspect the merged structure tree on the left and nested data on the right.
Sleep 1400ms

# Press G to jump to the orders element node and l to unfold its schema.
Type "G"
Type "l"
Sleep 1300ms

# Walk down to the nested items array so the right pane collects all 8 line items across all 6 orders.
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Sleep 1300ms

# Switch to Table view to display every line item from every order in one unified table.
Type "1"
Sleep 1400ms

# Switch to Pretty view to inspect the collected line items as formatted JSON.
Type "3"
Sleep 1300ms

# Section: Multi-Depth Mark & Flatten with Leaf Names
# Move up to order_id and mark it with Space.
Type "k"
Type "k"
Type "k"
Type "k"
Type "k"
Space
Sleep 1100ms

# Expand customer and mark email at depth 3.
Type "j"
Type "j"
Type "j"
Type "j"
Type "l"
Type "j"
Type "j"
Type "j"
Space
Sleep 1100ms

# Expand items[] and mark sku and qty at depth 4.
Type "j"
Type "j"
Type "l"
Type "j"
Type "l"
Type "j"
Space
Type "j"
Type "j"
Space
Sleep 1200ms

# Press F to flatten the marked fields into Table view using clean original leaf names.
Type "F"
Sleep 1500ms

# Undo flatten with u to return to the original nested structure.
Type "u"
Sleep 900ms

# Section: Hoist, Splat, Nest & Convert Tree Shapes
# Expand orders[].customer.address and focus city at level 4.
Type "g"
Type "g"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "l"
Type "j"
Sleep 900ms

# Press < to hoist city up one level from address directly into customer across every order.
Type "<"
Sleep 1400ms

# Press < on the address object to splat its remaining keys into customer and drop the empty wrapper.
Type "<"
Sleep 1400ms

# Move up from customer onto priority.
Type "k"
Sleep 600ms

# Press > on priority to open the Nest picker listing sibling object customer.
Type ">"
Sleep 1400ms

# Press Enter to move priority directly into the existing customer object across every order.
Enter
Sleep 1300ms

# Undo the nest, splat, and hoist steps cleanly with u.
Type "u"
Type "u"
Type "u"
Sleep 800ms

# Focus store in the structure tree.
Type "g"
Type "g"
Type "j"
Sleep 600ms

# Open the Convert Shape picker with C on store.
Type "C"
Sleep 1400ms

# Convert store into an entries array so keys and values appear as rows.
Enter
Enter
Sleep 1300ms

# Undo the store conversion.
Type "u"
Sleep 700ms

# Section: Side-by-Side Split Transfer
# Press M on store to open a side-by-side split and transfer store into reshaped.json.
Type "M"
Sleep 1500ms

# Switch to the right-hand reshaped.json window with ctrl+w w.
Ctrl+W
Type "w"
Sleep 1200ms

# Undo the transfer inside reshaped.json, then close the split with ctrl+w o.
Type "u"
Ctrl+W
Type "w"
Ctrl+W
Type "o"
Tab
Sleep 1000ms

# Section: Filter, Sort & Group Rows
# Focus orders in Table view and jump into the table pane.
Type "g"
Type "g"
Type "j"
Type "j"
Type "j"
Type "j"
Type "j"
Type "1"
Tab
Type "0"
Sleep 900ms

# Filter across all columns with / to keep only shipped orders.
Type "/"
Type "shipped"
Enter
Sleep 1400ms

# Move onto the amount column before adding a field filter.
Type "l"
Type "l"
Sleep 700ms

# Stack a numeric field filter (>100) on amount with f.
Type "f"
Type ">100"
Enter
Sleep 1400ms

# Undo both filters to restore all six orders.
Type "u"
Type "u"
Sleep 1000ms

# Press s twice on amount to sort ascending and then flip to descending.
Type "s"
Type "s"
Sleep 1300ms

# Move left onto the status column.
Type "h"
Sleep 600ms

# Group rows by status into buckets with g b.
Type "g"
Type "b"
Sleep 1400ms

# Undo the grouping and sort steps to restore original order.
Type "u"
Type "u"
Sleep 800ms

# Section: Interactive jq Builder & Live Preview
# Open the live jq bar with J and type | .orders to target the orders array.
Type "J"
Type " | .orders"
Sleep 1400ms

# Press Tab inside the jq bar to open the interactive stage picker with descriptions and snippets.
Tab
Sleep 1500ms

# Select count/length from the stage picker to append | length and preview the live count.
Type "count"
Enter
Sleep 1400ms

# Press Enter to open the jq result in a new editable buffer, then return with [ b.
Enter
Sleep 1300ms

# Switch back to orders.json with ctrl+6.
Ctrl+6
Sleep 1100ms

# Section: Raw Folding, Visual Selection & Yanking
# Open Raw JSON view and focus the Vim source editor.
Type "4"
Sleep 1200ms

# Fold all nested JSON closures at once with zM.
Type "z"
Type "M"
Sleep 1300ms

# Unfold all closures with zR, then toggle just the store object with j and za.
Type "z"
Type "R"
Type "j"
Type "z"
Type "a"
Sleep 1400ms

# Unfold store, switch to Nested view, and focus the collapsed order rows.
Type "z"
Type "R"
Tab
Type "2"
Tab
Type "g"
Type "g"
Type "j"
Type "j"
Type "j"
Type "h"
Sleep 900ms

# Select three orders in Visual mode with v and batch-unfold them with l.
Type "v"
Type "j"
Type "j"
Type "l"
Sleep 1400ms

# Yank the focused JSON value with y y and its exact path with y p.
Type "y"
Type "y"
Type "y"
Type "p"
Sleep 1300ms

# Open context-aware key help with ? to review active bindings.
Type "?"
Sleep 1400ms

# Dismiss key help before opening the command palette.
Escape
Sleep 600ms

# Open the : command palette to search every action and Vim-style ex command.
Type ":"
Sleep 1400ms

