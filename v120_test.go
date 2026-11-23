package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"ofam.dev/ochazuke/config"
)

const v120Doc = `{
  "store": {"name": "Tea Co", "region": "us-west"},
  "orders": [
    {
      "order_id": "O1",
      "status": "shipped",
      "amount": 150,
      "priority": "high",
      "customer": {"name": "Maya", "email": "maya@ex.com", "address": {"city": "Seattle", "zip": "98101"}},
      "items": []
    },
    {
      "order_id": "O2",
      "status": "pending",
      "amount": 60,
      "priority": "low",
      "customer": {"name": "Jonas", "email": "jonas@ex.com", "address": {"city": "Portland", "zip": "97204"}},
      "items": [{"sku": "S1", "qty": 2}, {"sku": "S2", "qty": 1}]
    },
    {
      "order_id": "O3",
      "status": "shipped",
      "amount": 90,
      "priority": "normal",
      "customer": {"name": "Priya", "email": null, "address": {"city": "Tacoma", "zip": "98402"}},
      "items": [{"sku": "S3", "qty": 4}]
    }
  ]
}`

func newV120Driver(t *testing.T, raw string) (*App, *driver) {
	t.Helper()
	b, err := NewBuffer("orders.json", raw, false)
	if err != nil {
		t.Fatal(err)
	}
	a := newApp(Config{Copy: func(string) error { return nil }}, b)
	return a, newDriver(a.shell(), 140, 36)
}

func treeWalkTo(a *App, d *driver, targetPath string) bool {
	if a.pane != 0 {
		d.keys("tab")
	}
	d.keys("g", "g")
	for range 120 {
		rows := a.structRows(a.cur())
		i := a.curUI().cursor
		if i < 0 || i >= len(rows) {
			return false
		}
		r := rows[i]
		if r.s.Path.String() == targetPath {
			return true
		}
		if len(r.s.Children) > 0 && !r.open {
			d.keys("l")
		} else if i == len(rows)-1 {
			return false
		} else {
			d.keys("j")
		}
	}
	return false
}

func TestV120BindingsCheckAndSuiteKeys(t *testing.T) {
	diags := config.CheckBindings(modes, defaultBindings(), commandSpecs()...)
	for _, d := range diags {
		if d.Severity == config.SevError {
			t.Errorf("binding check error #%d: %s", d.Index, d.Message)
		}
	}
	for _, b := range defaultBindings() {
		if strings.Join(b.Keys, " ") == "g G" {
			t.Errorf("g G should be removed, found binding to %s", b.Command)
		}
	}

	a, d := newV120Driver(t, v120Doc)
	// G moves to bottom of structure tree (motion.bottom).
	d.keys("G")
	rows := a.structRows(a.cur())
	if a.curUI().cursor != len(rows)-1 {
		t.Fatalf("G cursor = %d, want %d", a.curUI().cursor, len(rows)-1)
	}
	// g B opens buffer picker.
	d.keys("g", "B")
	if pm, _ := modalData(d.sh.State, modalPick).(*pickModal); pm == nil || pm.kind != "buffers" {
		t.Fatalf("g B did not open buffers picker: %+v", pm)
	}
	d.keys("esc")

	// g b groups by the focused field.
	if !treeWalkTo(a, d, "orders[].status") {
		t.Fatal("could not reach orders[].status")
	}
	d.keys("g", "b")
	if len(a.cur().Pipeline) == 0 || a.cur().Pipeline[1].Kind != "group" {
		t.Fatalf("g b did not group: %+v (status %q)", a.cur().Pipeline, a.status)
	}
}

func TestV120DeepArrayPreviewAcrossAllParents(t *testing.T) {
	// Notice orders[0].items is EMPTY ([]), while orders[1] has S1,S2 and orders[2] has S3.
	a, d := newV120Driver(t, v120Doc)
	if !treeWalkTo(a, d, "orders[].items[]") {
		t.Fatal("could not reach orders[].items[]")
	}

	// Nested view immediately previews all 3 items across orders[1] and orders[2].
	frame := d.frame()
	if !strings.Contains(frame, "orders[].items[]") || !strings.Contains(frame, "3 items") {
		t.Fatalf("nested deep preview missing all items:\n%s", frame)
	}

	// Table view shows all 3 rows across all parents, not 0 rows from orders[0]!
	d.keys("1")
	rel, root := a.relation(a.active(), a.cur())
	if root.String() != "orders[].items" || len(rel.Rows) != 3 {
		t.Fatalf("table deep preview got root=%s rows=%d, want orders[].items and 3 rows", root, len(rel.Rows))
	}
	frame = d.frame()
	for _, sku := range []string{"S1", "S2", "S3"} {
		if !strings.Contains(frame, sku) {
			t.Errorf("table deep preview missing %s:\n%s", sku, frame)
		}
	}

	// Stepping into Table pane lets j/G navigate across parent order boundaries!
	d.keys("tab", "G")
	if got := a.curUI().focus.String(); !strings.HasPrefix(got, "orders[2].items[0]") {
		t.Fatalf("G in deep table focus = %q, want orders[2].items[0].*", got)
	}
	d.keys("tab")

	// Pretty view also shows all 3 items collected across all parents.
	if !treeWalkTo(a, d, "orders[].items[].sku") {
		t.Fatal("could not reach orders[].items[].sku")
	}
	d.keys("3")
	frame = d.frame()
	for _, want := range []string{"orders[].items[]", "S1", "S2", "S3"} {
		if !strings.Contains(frame, want) {
			t.Errorf("pretty deep preview missing %q:\n%s", want, frame)
		}
	}
}

func TestV120FlattenLeafNamesAndCollisionDisambiguation(t *testing.T) {
	// Unique leaf names across depths keep their original leaf key names.
	doc := MustParse(v120Doc)
	r, err := Flatten(doc, paths("orders[].order_id", "orders[].customer.email", "orders[].items[].sku", "orders[].items[].qty"), nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	wantCols := "order_id,email,sku,qty"
	if got := strings.Join(r.Columns, ","); got != wantCols {
		t.Fatalf("unique leaf columns = %q, want %q", got, wantCols)
	}

	// Colliding leaf names disambiguate with minimal parent prefix only for colliding fields.
	r2, err := Flatten(doc, paths("store.name", "orders[].customer.name", "orders[].order_id", "orders[].items[].sku"), nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	wantCols2 := "store.name,customer.name,order_id,sku"
	if got := strings.Join(r2.Columns, ","); got != wantCols2 {
		t.Fatalf("colliding leaf columns = %q, want %q", got, wantCols2)
	}

	// Multi-level collision expands only until unique.
	deepCollision := MustParse(`{"a":{"x":{"id":1},"y":2},"b":{"x":{"id":3}}}`)
	r3, err := Flatten(deepCollision, nil, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r3.Columns, ","); got != "a.x.id,y,b.x.id" {
		t.Fatalf("deep collision columns = %q, want a.x.id,y,b.x.id", got)
	}

	// Interactive Space mark + F automatically switches to Table view with leaf names.
	a, d := newV120Driver(t, v120Doc)
	a.active().view = viewNested
	for _, p := range []string{"orders[].order_id", "orders[].customer.email", "orders[].items[].sku", "orders[].items[].qty"} {
		if !treeWalkTo(a, d, p) {
			t.Fatalf("could not walk to %s", p)
		}
		d.keys("space")
	}
	d.keys("F")
	if a.active().view != viewTable {
		t.Fatalf("F did not switch to viewTable, got view %d", a.active().view)
	}
	rel, _ := a.relation(a.active(), a.cur())
	if got := strings.Join(rel.Columns, ","); got != wantCols {
		t.Fatalf("interactive flatten cols = %q, want %q", got, wantCols)
	}
}

func TestV120HoistAndNestJQParity(t *testing.T) {
	doc := MustParse(v120Doc)
	cases := []struct {
		name  string
		ops   []Operation
		check func(t *testing.T, v any)
		fails bool
	}{
		{
			name: "hoist scalar from level 4 to level 3",
			ops:  []Operation{{Kind: "scope", Path: MustPath("orders[].customer")}, {Kind: "hoist", Field: "address", Fields: []string{"city"}}},
			check: func(t *testing.T, v any) {
				if x, ok := AtPath(v, MustPath("orders[0].customer.city")); !ok || x != "Seattle" {
					t.Fatalf("city not hoisted: %v %v", x, ok)
				}
				if _, ok := AtPath(v, MustPath("orders[0].customer.address.city")); ok {
					t.Fatal("city still in address")
				}
				if x, ok := AtPath(v, MustPath("orders[0].customer.address.zip")); !ok || x != "98101" {
					t.Fatalf("zip missing from address: %v %v", x, ok)
				}
			},
		},
		{
			name: "hoist splat object into parent array elements",
			ops:  []Operation{{Kind: "scope", Path: MustPath("orders")}, {Kind: "hoist", Field: "customer"}},
			check: func(t *testing.T, v any) {
				if x, ok := AtPath(v, MustPath("orders[1].email")); !ok || x != "jonas@ex.com" {
					t.Fatalf("splat email missing: %v %v", x, ok)
				}
				if _, ok := AtPath(v, MustPath("orders[1].customer")); ok {
					t.Fatal("customer wrapper still present after splat")
				}
			},
		},
		{
			name: "hoist all keys removes empty wrapper",
			ops:  []Operation{{Kind: "scope", Path: MustPath("orders[].customer")}, {Kind: "hoist", Field: "address", Fields: []string{"city", "zip"}}},
			check: func(t *testing.T, v any) {
				if _, ok := AtPath(v, MustPath("orders[0].customer.address")); ok {
					t.Fatal("empty address wrapper should be removed")
				}
				if x, _ := AtPath(v, MustPath("orders[2].customer.zip")); x != "98402" {
					t.Fatalf("zip = %v, want 98402", x)
				}
			},
		},
		{
			name: "nest into existing sibling object",
			ops:  []Operation{{Kind: "scope", Path: MustPath("orders")}, {Kind: "nest", Name: "customer", Fields: []string{"priority", "status"}}},
			check: func(t *testing.T, v any) {
				if x, ok := AtPath(v, MustPath("orders[0].customer.priority")); !ok || x != "high" {
					t.Fatalf("priority not moved into customer: %v %v", x, ok)
				}
				if _, ok := AtPath(v, MustPath("orders[0].priority")); ok {
					t.Fatal("priority still at outer level")
				}
				if x, ok := AtPath(v, MustPath("orders[0].customer.name")); !ok || x != "Maya" {
					t.Fatalf("existing customer fields lost: %v %v", x, ok)
				}
			},
		},
		{
			name: "nest into new nested object",
			ops:  []Operation{{Kind: "scope", Path: MustPath("orders")}, {Kind: "nest", Name: "billing", Fields: []string{"amount", "status"}}},
			check: func(t *testing.T, v any) {
				if x, ok := AtPath(v, MustPath("orders[1].billing.amount")); !ok || valueText(x) != "60" {
					t.Fatalf("amount not moved into billing: %v %v", x, ok)
				}
				if _, ok := AtPath(v, MustPath("orders[1].amount")); ok {
					t.Fatal("amount still at outer level")
				}
			},
		},
		{
			name:  "nest into non-object sibling fails",
			ops:   []Operation{{Kind: "scope", Path: MustPath("orders")}, {Kind: "nest", Name: "order_id", Fields: []string{"priority"}}},
			fails: true,
		},
	}

	_, hasJQ := exec.LookPath("jq")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goOut, goErr := ApplyPipeline(doc, tc.ops)
			if (goErr != nil) != tc.fails {
				t.Fatalf("go err = %v, want fail=%v", goErr, tc.fails)
			}
			if !tc.fails && tc.check != nil {
				tc.check(t, goOut)
			}
			if hasJQ == nil {
				prog := SynthesizeJQ(tc.ops)
				jqOut, jqErr := RunJQ(context.Background(), v120Doc, prog)
				if (jqErr != nil) != tc.fails {
					t.Fatalf("jq err = %v (want fail=%v)\nprog: %s", jqErr, tc.fails, prog)
				}
				if !tc.fails {
					if got, want := compact(t, goOut), compact(t, jqOut); got != want {
						t.Fatalf("Go/jq mismatch:\ngo:   %s\njq:   %s\nprog: %s", got, want, prog)
					}
				}
			}
		})
	}
}

func TestV120InteractiveHoistNestTransferAndJQBuilder(t *testing.T) {
	a, d := newV120Driver(t, v120Doc)

	// 1. Interactive < (hoist scalar & splat object) + u undo.
	if !treeWalkTo(a, d, "orders[].customer.address.city") {
		t.Fatal("could not walk to orders[].customer.address.city")
	}
	d.keys("<")
	if x, ok := AtPath(a.display(a.cur()), MustPath("orders[0].customer.city")); !ok || x != "Seattle" {
		t.Fatalf("interactive < lift failed: %v %v (status %q)", x, ok, a.status)
	}
	d.keys("u")
	if _, ok := AtPath(a.display(a.cur()), MustPath("orders[0].customer.city")); ok {
		t.Fatal("u did not undo hoist")
	}

	if !treeWalkTo(a, d, "orders[].customer.address") {
		t.Fatal("could not walk to orders[].customer.address")
	}
	d.keys("<")
	if x, ok := AtPath(a.display(a.cur()), MustPath("orders[1].customer.zip")); !ok || x != "97204" {
		t.Fatalf("interactive < splat failed: %v %v", x, ok)
	}
	if _, ok := AtPath(a.display(a.cur()), MustPath("orders[1].customer.address")); ok {
		t.Fatal("address wrapper still exists after interactive splat")
	}
	d.keys("u")

	// 2. Interactive > (nest into existing sibling object + nest into new object by typing name).
	if !treeWalkTo(a, d, "orders[].priority") {
		t.Fatal("could not walk to orders[].priority")
	}
	d.keys(">")
	pm, _ := modalData(d.sh.State, modalPick).(*pickModal)
	if pm == nil || pm.kind != "nest" {
		t.Fatalf("> did not open nest picker: %+v (status %q)", pm, a.status)
	}
	// First item is existing sibling object "customer".
	d.keys("enter")
	if x, ok := AtPath(a.display(a.cur()), MustPath("orders[0].customer.priority")); !ok || x != "high" {
		t.Fatalf("> into existing sibling customer failed: %v %v", x, ok)
	}
	d.keys("u")

	// Nest marked fields into a brand-new object by typing a new key name.
	if !treeWalkTo(a, d, "orders[].amount") {
		t.Fatal("could not walk to orders[].amount")
	}
	d.keys("space")
	if !treeWalkTo(a, d, "orders[].priority") {
		t.Fatal("could not walk to orders[].priority")
	}
	d.keys("space", ">")
	d.typeText("billing")
	d.keys("enter")
	if x, ok := AtPath(a.display(a.cur()), MustPath("orders[0].billing.priority")); !ok || x != "high" {
		t.Fatalf("> into new object billing failed: %v %v (status %q)", x, ok, a.status)
	}
	d.keys("u")

	// 3. Interactive M (transfer across splits + undo + transfer back).
	if !treeWalkTo(a, d, "store") {
		t.Fatal("could not walk to store")
	}
	d.keys("M")
	if len(a.wins) != 2 {
		t.Fatalf("M did not open side-by-side split: wins=%d", len(a.wins))
	}
	dstBuf := a.buf(a.wins[1].buf)
	if x, ok := AtPath(a.preview(dstBuf), MustPath("store.name")); !ok || x != "Tea Co" {
		t.Fatalf("M did not transfer store to right buffer: %v %v", x, ok)
	}
	// Transfer a second field from left -> merges cleanly into right buffer.
	if !treeWalkTo(a, d, "orders[].order_id") {
		t.Fatal("could not walk to orders[].order_id")
	}
	d.keys("M")
	if x, ok := AtPath(a.preview(dstBuf), MustPath("order_id[2]")); !ok || x != "O3" {
		t.Fatalf("second M did not merge order_id into right buffer: %v %v", x, ok)
	}
	// Switch to right window, undo second transfer with u, verify store stays.
	d.keys("ctrl+w", "l", "u")
	if _, ok := AtPath(a.preview(dstBuf), MustPath("order_id")); ok {
		t.Fatal("u in right window did not undo second transfer")
	}
	if x, ok := AtPath(a.preview(dstBuf), MustPath("store.name")); !ok || x != "Tea Co" {
		t.Fatalf("store missing after undoing second transfer: %v %v", x, ok)
	}
	// Close split and return to left buffer.
	d.keys("ctrl+w", "h", "ctrl+w", "o")

	// 4. Rename & drop deeply nested field from structure pane.
	if !treeWalkTo(a, d, "orders[].customer.email") {
		t.Fatal("could not walk to orders[].customer.email")
	}
	d.keys("x")
	if _, ok := AtPath(a.display(a.cur()), MustPath("orders[0].customer.email")); ok {
		t.Fatal("x did not drop orders[].customer.email")
	}
	d.keys("u")

	// 5. Interactive jq builder: chips + Tab stage picker.
	if _, err := exec.LookPath("jq"); err == nil {
		d.keys("J")
		frame := d.frame()
		for _, want := range []string{"alt+f filter", "alt+s select", "alt+l flatten", "alt+o sort_by", "alt+g group_by", "alt+k keys", "alt+u unique_by", "alt+c count"} {
			if !strings.Contains(frame, want) {
				t.Errorf("jq chip bar missing %q:\n%s", want, frame)
			}
		}
		d.typeText(" | .orders")
		d.keys("tab")
		pm, _ = modalData(d.sh.State, modalPick).(*pickModal)
		if pm == nil || pm.kind != "jqbuilder" {
			t.Fatalf("Tab inside jq bar did not open jqbuilder picker: %+v", pm)
		}
		d.typeText("count")
		d.keys("enter")
		if !strings.Contains(a.prompt.ed.Value(), "| length") || valueText(a.jq.val) != "3" {
			t.Fatalf("jq builder stage picker failed: expr=%q val=%v err=%q", a.prompt.ed.Value(), a.jq.val, a.jq.err)
		}
		d.keys("esc")
		// Also test direct alt+k (keys) chip.
		d.keys("J", "alt+k")
		if !strings.Contains(a.prompt.ed.Value(), "| keys") || !a.jq.ok {
			t.Fatalf("alt+k chip failed: expr=%q ok=%v err=%q", a.prompt.ed.Value(), a.jq.ok, a.jq.err)
		}
		d.keys("esc")
	}
}
