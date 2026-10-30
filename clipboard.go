package main

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// clipboardID is the instance that reports a failed copy.
const clipboardID = "clipboard"

// clipFailed is a copy the clipboard tool refused.
type clipFailed struct{ err error }

// clipNotices turns a failed copy into a notice: the registers still hold
// the text, so it is no error.
type clipNotices struct{ a *App }

func (c clipNotices) HandleMessage(msg tea.Msg) bool {
	m, ok := msg.(clipFailed)
	if ok {
		c.a.r.UI().Notify("clipboard: " + m.err.Error() + " (the registers still have it)")
	}
	return ok
}

func (clipNotices) PendingCommand() tea.Cmd { return nil }
func (clipNotices) Close()                  {}

// registerClipboard makes App.Clipboard ochazuke's way to the system
// clipboard (Registers.Copy): every yank goes through the registers
// (Runtime.StoreYank), and ochazuke copies the ones the clipboard setting
// (none, yanks, unnamed) or the "+ register sends there, off the loop.
// Without a clipboard tool ochazuke's own OSC 52 copy is used.
func (a *App) registerClipboard() {
	a.r.Instance(clipboardID).Component = clipNotices{a}
	a.r.Registers().Copy = func(text string) tea.Cmd {
		clip := a.Clipboard
		if clip == nil {
			return tea.SetClipboard(text)
		}
		return func() tea.Msg {
			if err := clip(text); err != nil {
				return clipFailed{err}
			}
			return nil
		}
	}
}

var _ ochazuke.Component = clipNotices{}

// systemClipboard is a copy function for the first clipboard tool on PATH
// (pbcopy, wl-copy, xclip, xsel), or nil when there is none. Copies run off
// the loop (a tea.Cmd, registerClipboard), so a slow tool never stalls keys.
func systemClipboard() func(string) error {
	for _, tool := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		args := tool[1:]
		return func(text string) error {
			cmd := exec.Command(path, args...)
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		}
	}
	return nil
}

// systemPaste is a read function for the first clipboard tool on PATH
// (pbpaste, wl-paste, xclip, xsel), or nil when there is none. viewer.paste
// runs it off the loop.
func systemPaste() func() (string, error) {
	for _, tool := range [][]string{{"pbpaste"}, {"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"xsel", "--clipboard", "--output"}} {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		args := tool[1:]
		return func() (string, error) {
			out, err := exec.Command(path, args...).Output()
			return string(out), err
		}
	}
	return nil
}
