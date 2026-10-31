package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
)

// viewer.open-in hands the table to o-csv in the foreground: a temporary
// csv, removed once the program exits; without o-csv on PATH it is not
// available.
func TestOpenInHandsTheTableToOCSV(t *testing.T) {
	defer func(f func(string) (string, error)) { lookPath = f }(lookPath)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	a := open(t, sample, 120, 30)
	a.on(t, ".items[].owner.name")
	if a.available("viewer", "open-in") {
		t.Fatal("open-in available without o-csv")
	}
	lookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	if !a.available("viewer", "open-in") {
		t.Fatal("open-in not available with o-csv on PATH")
	}
	var ran *exec.Cmd
	var during string
	a.m.Foreground = func(msg ochazuke.ForegroundMsg) tea.Cmd {
		ran = msg.Cmd
		b, _ := os.ReadFile(msg.Cmd.Args[1])
		during = string(b)
		return func() tea.Msg { return msg.Done(nil) }
	}
	if _, err := a.r.Execute(ochazuke.Request{Noun: "viewer", Verb: "open-in"}); err != nil {
		t.Fatal(err)
	}
	settle(a)
	if ran == nil || ran.Args[0] != "/bin/o-csv" || !strings.HasSuffix(ran.Args[1], "sample.csv") {
		t.Fatalf("ran %v", ran)
	}
	if !strings.HasPrefix(during, "id,name,owner.name,") {
		t.Fatalf("the csv o-csv read: %q", during)
	}
	settle(a)
	if _, err := os.Stat(ran.Args[1]); !os.IsNotExist(err) {
		t.Fatalf("the temporary csv is still there: %v", err)
	}
}
