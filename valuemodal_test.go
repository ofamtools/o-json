package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// inlineEditor sets value.editor inline: the tests that draw the inline
// input over the row.
func inlineEditor(t *testing.T, a *App) {
	t.Helper()
	if err := a.r.Settings().Set(settingValueEditor, editorInline); err != nil {
		t.Fatal(err)
	}
}

// modalBox is the value editor's box in a frame: its rows and columns
// (the border included); ok false when no editor is drawn.
func modalBox(frame, title string) (top, bottom, left, right int, ok bool) {
	lines := strings.Split(frame, "\n")
	for y, l := range lines {
		if !strings.Contains(l, "│ "+title) {
			continue
		}
		left = runeIndex(l, "│ "+title)
		top = y - 1
		above := []rune(lines[top])
		right = left
		for right+1 < len(above) && above[right+1] == '─' {
			right++
		}
		right++
		for bottom = y; bottom < len(lines) && []rune(lines[bottom])[left] != '╰'; bottom++ {
		}
		return top, bottom, left, right, true
	}
	return 0, 0, 0, 0, false
}

// runeIndex is the rune offset of sub in s.
func runeIndex(s, sub string) int {
	i := strings.Index(s, sub)
	if i < 0 {
		return -1
	}
	return len([]rune(s[:i]))
}

// outside is a frame with the box's cells blanked, and its first rows
// (the mode badge) and last rows (the footer's hints) dropped: what the
// page draws around the modal.
func outside(frame string, top, bottom, left, right int) string {
	lines := strings.Split(frame, "\n")
	var out []string
	for y, l := range lines {
		if y < 3 || y >= len(lines)-3 {
			continue
		}
		r := []rune(l)
		if y >= top && y <= bottom {
			for x := left; x <= right && x < len(r); x++ {
				r[x] = ' '
			}
		}
		out = append(out, string(r))
	}
	return strings.Join(out, "\n")
}

// assertBehind fails unless the page drawn around the open editor is the
// page before it opened, cell for cell: the table does not reflow.
func assertBehind(t *testing.T, before, during, title string) {
	t.Helper()
	top, bottom, left, right, ok := modalBox(during, title)
	if !ok {
		t.Fatalf("no editor titled %q\n%s", title, during)
	}
	if got, want := outside(during, top, bottom, left, right), outside(before, top, bottom, left, right); got != want {
		t.Fatalf("the page moved behind the editor\nbefore\n%s\nduring\n%s", before, during)
	}
}

// r on a table cell opens the value editor over the table, titled with
// the value's path, the old value dimmed, the text selected and its type
// as typed; enter sets it (one undo step) and the cursor stays on the
// cell; esc goes to normal mode (the text kept), where q cancels and a
// second esc leaves as value.leave says; unchanged text is no edit.
func TestValueModalEditsACell(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[1]")
	press(t, a, "l", "l") // name
	before := view(a)
	press(t, a, "r")
	if a.r.TopModal() != valueEditorID || a.r.Focus != valueFieldID || a.r.Mode.Name != "insert" {
		t.Fatalf("r: modal %q focus %q mode %q", a.r.TopModal(), a.r.Focus, a.r.Mode.Name)
	}
	if sel := selected(a); sel != "beta" {
		t.Fatalf("r did not select the text: %q", sel)
	}
	frame := view(a)
	assertBehind(t, before, frame, ".items[1].name")
	for _, want := range []string{"was    beta", "value  beta", "enter set", "esc normal"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q in the editor\n%s", want, frame)
		}
	}
	if line(frame, "value  beta") == "" || !strings.Contains(line(frame, "value  beta"), " string ") {
		t.Fatalf("no type badge\n%s", frame)
	}
	typeText(t, a, "42")
	if l := line(view(a), "value  42"); !strings.Contains(l, " number ") {
		t.Fatalf("the badge does not follow the text: %q", l)
	}
	assertBehind(t, before, view(a), ".items[1].name")
	press(t, a, "enter")
	if got := field(t, a, ".items[1].name"); got != "42" {
		t.Fatalf("set: %s", got)
	}
	if a.r.TopModal() != "" || a.r.Mode.Name != "normal" || a.data.Selected() != 1 || a.data.list.Column() != 2 {
		t.Fatalf("after enter: modal %q mode %q row %d column %d", a.r.TopModal(), a.r.Mode.Name, a.data.Selected(), a.data.list.Column())
	}
	if len(a.rs.patches) != 1 {
		t.Fatalf("%d patches, want 1", len(a.rs.patches))
	}
	// esc goes to normal mode, the typing kept; q there cancels:
	// nothing changes.
	press(t, a, "r")
	typeText(t, a, "gone")
	press(t, a, "esc")
	if a.r.TopModal() != valueEditorID || a.r.Mode.Name != "normal" || !strings.Contains(view(a), "value  gone") {
		t.Fatalf("esc after typing: modal %q mode %q\n%s", a.r.TopModal(), a.r.Mode.Name, view(a))
	}
	press(t, a, "q")
	if got := field(t, a, ".items[1].name"); got != "42" || len(a.rs.patches) != 1 || a.r.TopModal() != "" {
		t.Fatalf("q: %s, %d patches, modal %q", got, len(a.rs.patches), a.r.TopModal())
	}
	// With value.leave cancel a second esc cancels too.
	if err := a.r.Settings().Set(settingValueLeave, leaveCancel); err != nil {
		t.Fatal(err)
	}
	press(t, a, "r")
	typeText(t, a, "gone")
	press(t, a, "esc", "esc")
	if got := field(t, a, ".items[1].name"); got != "42" || len(a.rs.patches) != 1 || a.r.TopModal() != "" {
		t.Fatalf("esc esc with value.leave cancel: %s, %d patches, modal %q", got, len(a.rs.patches), a.r.TopModal())
	}
	if err := a.r.Settings().Set(settingValueLeave, leaveSet); err != nil {
		t.Fatal(err)
	}
	// value.leave set (the default): a second esc sets, one undo step.
	press(t, a, "r")
	typeText(t, a, "kept")
	press(t, a, "esc", "esc")
	if got := field(t, a, ".items[1].name"); got != `"kept"` || len(a.rs.patches) != 2 || a.r.TopModal() != "" {
		t.Fatalf("esc esc with value.leave set: %s, %d patches, modal %q", got, len(a.rs.patches), a.r.TopModal())
	}
	press(t, a, "u")
	// Unchanged text is no edit; u undoes the one step.
	press(t, a, "r", "enter")
	if len(a.rs.patches) != 1 {
		t.Fatalf("an unchanged set made a patch (%d)", len(a.rs.patches))
	}
	press(t, a, "u")
	if got := field(t, a, ".items[1].name"); got != `"beta"` || len(a.rs.patches) != 0 {
		t.Fatalf("u: %s, %d patches", got, len(a.rs.patches))
	}
	agrees(t, a)
}

// rename.select off opens the editor with the cursor at the end of the
// text, nothing selected, so typing appends.
func TestValueModalRespectsRenameSelect(t *testing.T) {
	a := open(t, sample, 120, 30)
	if err := a.r.Settings().Set(ochazuke.RenameSelectSetting, false); err != nil {
		t.Fatal(err)
	}
	onData(t, a, ".", "version")
	press(t, a, "r")
	if sel := selected(a); sel != "" {
		t.Fatalf("selected %q with rename.select off", sel)
	}
	typeText(t, a, "1")
	press(t, a, "enter")
	if got := field(t, a, ".version"); got != "21" {
		t.Fatalf("appended: %s", got)
	}
}

// In a hover sub-table the value editor opens on the line's cell with the
// same keys as on a row: the table and its sub-table stay drawn behind
// it, and after enter the walk is still on the edited line.
func TestValueModalEditsASubTableCell(t *testing.T) {
	a := hoverAt(t, 120, 30, 6)
	press(t, a, "j") // the address line, on city
	if !a.hoverIn() {
		t.Fatal("j did not step into the sub-table")
	}
	before := view(a)
	press(t, a, "r")
	if a.r.Focus != valueFieldID || selected(a) != "Austin" {
		t.Fatalf("r: focus %q selected %q", a.r.Focus, selected(a))
	}
	assertBehind(t, before, view(a), "[2].address.city")
	typeText(t, a, "Lyon")
	press(t, a, "enter")
	if got := field(t, a, "[2].address.city"); got != `"Lyon"` {
		t.Fatalf("city: %s", got)
	}
	if !a.hoverIn() || !strings.Contains(view(a), "Lyon  73301") {
		t.Fatalf("the walk did not stay on the edited line (in %v)\n%s", a.hoverIn(), view(a))
	}
	// K renames the line's key, o adds a key and value beside it.
	press(t, a, "K")
	if a.r.Focus != valueKeyID || selected(a) != "city" {
		t.Fatalf("K: focus %q selected %q", a.r.Focus, selected(a))
	}
	press(t, a, "esc", "esc") // normal, then leave: the key unchanged is no edit
	if !a.hoverIn() || a.r.TopModal() != "" {
		t.Fatalf("esc on K: in %v modal %q", a.hoverIn(), a.r.TopModal())
	}
	press(t, a, "u")
	if got := field(t, a, "[2].address.city"); got != `"Austin"` {
		t.Fatalf("u: %s", got)
	}
}

// o in an object asks the key then the value in one editor: the key's
// placeholder selected, enter (or tab) moves on to the value (null
// selected), shift+tab back, enter sets both as one add, one undo step.
// A taken key says the key it will add.
func TestValueModalAddsAKeyAndValue(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "name")
	press(t, a, "o")
	if a.r.Focus != valueKeyID || selected(a) != "new-key" {
		t.Fatalf("o: focus %q selected %q", a.r.Focus, selected(a))
	}
	frame := view(a)
	for _, want := range []string{". · add after name", "key    new-key", "value  null", "enter next"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("no %q\n%s", want, frame)
		}
	}
	typeText(t, a, "version")
	if !strings.Contains(view(a), `adds the key "version-2"`) {
		t.Fatalf("a taken key is not noted\n%s", view(a))
	}
	press(t, a, "ctrl+u")
	typeText(t, a, "nick")
	press(t, a, "tab")
	if a.r.Focus != valueFieldID || selected(a) != "null" {
		t.Fatalf("tab: focus %q selected %q", a.r.Focus, selected(a))
	}
	press(t, a, "shift+tab")
	if a.r.Focus != valueKeyID || string(a.vedit.in.Text) != "nick" {
		t.Fatalf("shift+tab: focus %q text %q", a.r.Focus, a.vedit.in.Text)
	}
	press(t, a, "enter")
	typeText(t, a, "nj")
	if !strings.Contains(view(a), "enter set") {
		t.Fatalf("the value field does not set\n%s", view(a))
	}
	press(t, a, "enter")
	if got := field(t, a, "."); !strings.HasPrefix(got, `{"name":"o-json sample","nick":"nj","version":2`) {
		t.Fatalf("add: %s", got)
	}
	if len(a.rs.patches) != 1 || a.rs.patches[0].After != "name" {
		t.Fatalf("one add patch after name: %+v", a.rs.patches)
	}
	// O on an array row asks the value alone, before the row.
	onData(t, a, ".items", ".items[1]")
	press(t, a, "O")
	if a.r.Focus != valueFieldID || !strings.Contains(view(a), ".items · add before [1]") {
		t.Fatalf("O on an item: focus %q\n%s", a.r.Focus, view(a))
	}
	typeText(t, a, `{"id": 9}`)
	press(t, a, "enter")
	if got := field(t, a, ".items[1]"); got != `{"id":9}` {
		t.Fatalf("item before: %s", got)
	}
	agrees(t, a)
}

// K edits the key: a key the object has is numbered past its keys, as o
// names a key, and the editor says so before enter; enter always sets
// (record first, never refuse).
func TestValueModalRenameNumbersATakenKey(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".", "name")
	press(t, a, "K")
	if a.r.Focus != valueKeyID || selected(a) != "name" || !strings.Contains(view(a), ".name · rename key") {
		t.Fatalf("K: focus %q selected %q\n%s", a.r.Focus, selected(a), view(a))
	}
	typeText(t, a, "version")
	if !strings.Contains(view(a), `· renames to the key "version-2"`) {
		t.Fatalf("no note for a taken key:\n%s", view(a))
	}
	press(t, a, "enter")
	if got := field(t, a, "."); !strings.HasPrefix(got, `{"version-2":"o-json sample","version":2`) || a.r.TopModal() != "" {
		t.Fatalf("rename to a taken key: %s modal %q", got, a.r.TopModal())
	}
}

// An object or array value opens as indented JSON in a several-line
// input: enter is a new line, esc normal mode, enter there sets; JSON
// that does not parse is noted (it would be set as a string).
func TestValueModalEditsAContainerOnSeveralLines(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[0]")
	press(t, a, "l", "l", "l") // owner
	press(t, a, "r")
	if sel := selected(a); !strings.HasPrefix(sel, "{\n") {
		t.Fatalf("the JSON is not selected: %q", sel)
	}
	press(t, a, "esc", "esc") // normal, then leave: the text unchanged is no edit
	valueDo(t, a, "edit", ochazuke.Arguments{"select": false})
	in := a.vedit.in
	if !in.Multiline || !strings.Contains(string(in.Text), "\n  \"name\": \"ann\"") {
		t.Fatalf("not several lines: %q", in.Text)
	}
	if !strings.Contains(view(a), "esc normal, then enter sets") {
		t.Fatalf("no hint for the several-line input\n%s", view(a))
	}
	press(t, a, "backspace") // the closing "}"
	if !strings.Contains(view(a), "not valid JSON: sets it as a string") {
		t.Fatalf("broken JSON is not noted\n%s", view(a))
	}
	typeText(t, a, "}")
	press(t, a, "enter")
	if a.r.TopModal() != valueEditorID {
		t.Fatal("enter left the several-line input")
	}
	press(t, a, "backspace", "esc")
	if a.r.Mode.Name != "normal" || a.r.TopModal() != valueEditorID {
		t.Fatalf("esc: mode %q modal %q", a.r.Mode.Name, a.r.TopModal())
	}
	press(t, a, "enter")
	if got := field(t, a, ".items[0].owner"); got != `{"name":"ann","email":"ann@example.com"}` || len(a.rs.patches) != 0 {
		t.Fatalf("unchanged container: %s, %d patches", got, len(a.rs.patches))
	}
	valueDo(t, a, "edit", ochazuke.Arguments{"select": false})
	press(t, a, "esc", "g", "g", "j", "$")
	press(t, a, "a")
	typeText(t, a, "x")
	press(t, a, "esc", "enter")
	if got := field(t, a, ".items[0].owner"); got != `"{\n  \"name\": \"ann\",x\n  \"email\": \"ann@example.com\"\n}"` {
		t.Fatalf("broken JSON set: %s", got)
	}
}

// The value editor opens over the detail, which stays drawn behind it;
// u and ctrl+r undo and redo there, and the footer lists them.
func TestValueModalInTheDetail(t *testing.T) {
	a := open(t, sample, 120, 30)
	onData(t, a, ".items", ".items[1]")
	press(t, a, "enter", "G", "o")
	if a.r.TopModal() != valueEditorID || !strings.Contains(view(a), ".items[1] · add after archived") {
		t.Fatalf("o in the detail\n%s", view(a))
	}
	typeText(t, a, "priority")
	press(t, a, "enter")
	typeText(t, a, "high")
	press(t, a, "enter")
	if a.r.TopModal() != detailID || !strings.Contains(view(a), "priority  string     high") {
		t.Fatalf("after the add\n%s", view(a))
	}
	if !strings.Contains(view(a), "u/ctrl+r undo/redo") {
		t.Fatalf("the detail's footer does not list undo\n%s", view(a))
	}
	press(t, a, "u")
	if strings.Contains(view(a), "priority") || len(a.rs.patches) != 0 || a.r.TopModal() != detailID {
		t.Fatalf("u in the detail: %d patches\n%s", len(a.rs.patches), view(a))
	}
	press(t, a, "ctrl+r")
	if !strings.Contains(view(a), "priority  string     high") || len(a.rs.patches) != 1 {
		t.Fatalf("ctrl+r in the detail: %d patches\n%s", len(a.rs.patches), view(a))
	}
}

// value.editor inline keeps the input drawn over the row.
func TestValueEditorInlineSetting(t *testing.T) {
	a := open(t, sample, 120, 30)
	inlineEditor(t, a)
	onData(t, a, ".", "version")
	press(t, a, "r")
	if a.r.TopModal() != "" || a.r.Focus != valueInputID || !strings.Contains(view(a), "version (number): 2") {
		t.Fatalf("inline: modal %q focus %q\n%s", a.r.TopModal(), a.r.Focus, view(a))
	}
	typeText(t, a, "3")
	press(t, a, "esc")
	if a.r.Focus != valueInputID || a.r.Mode.Name != "normal" || field(t, a, ".version") != "2" {
		t.Fatalf("inline esc: focus %q mode %q (normal mode, the text kept, nothing set)", a.r.Focus, a.r.Mode.Name)
	}
	press(t, a, "esc")
	if got := field(t, a, ".version"); got != "3" {
		t.Fatalf("inline esc esc sets (value.leave set): %s", got)
	}
}

// The value editor at 80 and 120 columns: an edit over the table, the
// key and value of an add.
func TestValueModalGolden(t *testing.T) {
	for _, width := range []int{80, 120} {
		a := open(t, sample, width, 24)
		onData(t, a, ".items", ".items[1]")
		press(t, a, "l", "l", "r")
		typeText(t, a, "omega")
		got := view(a)
		press(t, a, "esc", "q", "o") // normal mode, q cancels, o adds
		got += "\n" + view(a)
		golden := filepath.Join("testdata", "value-modal-"+itoa(width)+".golden")
		if os.Getenv("UPDATE_GOLDEN") != "" {
			_ = os.WriteFile(golden, []byte(got), 0o644)
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatal(err)
		}
		if got != string(want) {
			t.Errorf("%d columns\n%s\nwant\n%s", width, got, want)
		}
	}
}

// The value editor keeps a row for each note it can show
// (value.stable-height), so a note appearing as the text is typed (JSON
// that does not parse, a key the object has) never moves the box or its
// keys line; off, the box fits the notes it shows.
func TestValueModalHoldsStillWhileTyping(t *testing.T) {
	cases := []struct {
		name, title string
		open        func(a *App)
		typing      string
	}{
		{"edit", ".items[1].name", func(a *App) {
			onData(t, a, ".items", ".items[1]")
			press(t, a, "l", "l", "r")
		}, "{"},
		{"rename", ".name · rename key", func(a *App) {
			onData(t, a, ".", "name")
			press(t, a, "K")
		}, "version"},
		{"add", ". · add after name", func(a *App) {
			onData(t, a, ".", "name")
			press(t, a, "o")
		}, "version"},
	}
	for _, c := range cases {
		for _, stable := range []bool{true, false} {
			t.Run(c.name+" "+itoa(map[bool]int{true: 1}[stable]), func(t *testing.T) {
				a := open(t, sample, 120, 30)
				if err := a.r.Settings().Set(settingValueStableHeight, stable); err != nil {
					t.Fatal(err)
				}
				c.open(a)
				top, bottom, _, _, ok := modalBox(view(a), c.title)
				if !ok {
					t.Fatalf("no editor\n%s", view(a))
				}
				typeText(t, a, c.typing)
				if c.name == "add" {
					press(t, a, "tab")
					typeText(t, a, "[")
				}
				frame := view(a)
				if a.vedit == nil || a.vedit.modal == nil || len(a.vedit.modal.notes()) == 0 {
					t.Fatalf("no note\n%s", frame)
				}
				top2, bottom2, _, _, _ := modalBox(frame, c.title)
				if moved := top2 != top || bottom2 != bottom; moved == stable {
					t.Fatalf("stable %v: box rows %d-%d, then %d-%d\n%s", stable, top, bottom, top2, bottom2, frame)
				}
			})
		}
	}
}
