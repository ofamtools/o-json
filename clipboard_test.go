package main

import (
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// Every yank goes through ochazuke's registers, so the clipboard setting
// decides what reaches the system clipboard: none copies only the "+
// register, yanks (the default) every yank. Showing the jq program copies
// nothing; restructure.jq {yank} does (R9.8).
func TestClipboardSettingDecides(t *testing.T) {
	a := openOriginal(t, sample, 100, 30)
	var clip []string
	a.Clipboard = func(s string) error { clip = append(clip, s); return nil }
	key := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			finish(a, Press(a.m, k))
		}
	}
	if err := a.r.Settings().Set(ochazuke.ClipboardSetting, "none"); err != nil {
		t.Fatal(err)
	}
	selectPath(t, a, ".items[].owner.email")
	key("y", "y")
	if y, _ := a.r.ReadYank(""); y.Text != ".items[].owner.email" || len(clip) != 0 {
		t.Fatalf("clipboard none: register %q, clipboard %q", y.Text, clip)
	}
	key("\"", "+", "y", "y")
	if len(clip) != 1 || clip[0] != ".items[].owner.email" {
		t.Fatalf(`clipboard none, "+y: clipboard %q`, clip)
	}
	run := func(args ochazuke.Arguments) any {
		t.Helper()
		out, err := a.r.Execute(ochazuke.Request{Noun: "restructure", Verb: "jq", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		finish(a, a.r.PendingCommands())
		return out
	}
	before, _ := a.r.ReadYank("")
	if out, _ := run(nil).(string); out == "" {
		t.Fatal("restructure.jq answered no program")
	}
	if y, _ := a.r.ReadYank(""); y.Text != before.Text || len(clip) != 1 {
		t.Fatalf("restructure.jq copied: register %q, clipboard %q", y.Text, clip)
	}
	if err := a.r.Settings().Set(ochazuke.ClipboardSetting, "yanks"); err != nil {
		t.Fatal(err)
	}
	program, _ := run(ochazuke.Arguments{"yank": true}).(string)
	if y, _ := a.r.ReadYank(""); y.Text != program || len(clip) != 2 || clip[1] != program {
		t.Fatalf("restructure.jq {yank}: register %q, clipboard %d copies", y.Text, len(clip))
	}
	key("y", "y")
	if len(clip) != 3 || clip[2] != ".items[].owner.email" {
		t.Fatalf("clipboard yanks: clipboard %q", clip)
	}
}
