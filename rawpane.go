package main

// RAW in a tuios pane (Design D, raw.pane auto): inside tuios
// (TUIOS_ENV=1 with a session and a pane id) g r opens the editor in a
// new tuios window instead of suspending o-json: the window runs the
// editor on the temp file alone (its process, no shell; it closes when
// the editor exits), with the grants raw.pane-grants names. o-json keeps
// running and polls the window; when it is gone the file is read back as
// in the foreground (rawDone: no change, the diff as one undo step,
// invalid JSON reopens). While it runs the breadcrumb says RAW running,
// g r focuses the window again (re-attach), and the editor's ctrl+a d
// (raw.detach, an ex command passed with -c: nvim and vim) focuses
// o-json's pane again (detach). raw.pane foreground, or no tuios, runs
// the editor in the foreground as before.
//
// Every tuios call goes through App.tuios (a runner tests replace), and
// the environment through App.getenv.

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"ofam.dev/ochazuke"
)

const (
	settingRawPane   = "raw.pane"
	settingRawDetach = "raw.detach"
	settingRawGrants = "raw.pane-grants"

	rawPaneAuto       = "auto"
	rawPaneForeground = "foreground"

	// rawPanePoll is how often o-json asks tuios whether the editor's
	// window is still there.
	rawPanePoll = 300 * time.Millisecond

	rawDetachDefault = `nnoremap <silent> <C-a>d <Cmd>call system(['tuios', 'focus-window', '-s', '{session}', '{pane}'])<CR>`
)

// tuiosRunner runs tuios with args and returns its standard output.
type tuiosRunner func(args ...string) ([]byte, error)

func runTuios(args ...string) ([]byte, error) {
	out, err := exec.Command("tuios", args...).Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		err = errors.New(strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

// rawPaneDone says the editor's tuios window is gone.
type rawPaneDone struct{ window string }

// tuiosHere is the session and pane o-json runs in, when it runs in tuios.
func (a *App) tuiosHere() (session, pane string, ok bool) {
	session, pane = a.getenv("TUIOS_SESSION"), a.getenv("TUIOS_PANE_ID")
	return session, pane, a.getenv("TUIOS_ENV") == "1" && session != "" && pane != ""
}

// rawInPane reports RAW opens a tuios window (raw.pane auto inside tuios).
func (a *App) rawInPane() bool {
	_, _, ok := a.tuiosHere()
	return ok && a.r.Settings().String(settingRawPane) == rawPaneAuto
}

// runRawPane opens the editor (argv, its arguments ready) in a tuios
// window and starts watching it.
func (a *App) runRawPane(c ochazuke.Ctx, argv []string) ochazuke.Result {
	session, pane, _ := a.tuiosHere()
	args := []string{"new-window", "raw-" + appName(), "-s", session, "--print-id"}
	if g := strings.TrimSpace(a.r.Settings().String(settingRawGrants)); g != "" {
		args = append(args, "--grants", g)
	}
	// The window's process is the editor under sh, which leaves a marker
	// file when the editor exits: a tuios window may outlive its process,
	// so the marker, not the window, says the edit is over.
	done := a.raw.file + ".done"
	os.Remove(done)
	cmd := []string{"sh", "-c", `"$@"; s=$?; : > "$0"; exit $s`, done, argv[0]}
	if d := strings.TrimSpace(a.r.Settings().String(settingRawDetach)); d != "" {
		d = strings.NewReplacer("{session}", session, "{pane}", pane).Replace(d)
		cmd = append(cmd, "-c", d)
	}
	cmd = append(cmd, argv[1:]...)
	args = append(append(args, "--"), cmd...)
	out, err := a.tuios(args...)
	if err != nil {
		return ochazuke.Failed(errors.New("raw: tuios new-window: " + err.Error()))
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return ochazuke.Failed(errors.New("raw: tuios new-window printed no window id"))
	}
	a.raw.window = id
	a.later = append(a.later, a.watchRawPane(session, id, done))
	c.Info("RAW " + a.raw.path + " in a tuios pane · ctrl+a d there comes back · g r goes to it")
	return ochazuke.Done(nil)
}

// watchRawPane waits for the editor's end: its marker file (then the
// window is closed), or the window gone (closed by hand).
func (a *App) watchRawPane(session, id, done string) tea.Cmd {
	run := a.tuios
	return func() tea.Msg {
		for {
			time.Sleep(rawPanePoll)
			if _, err := os.Stat(done); err == nil {
				os.Remove(done)
				_, _ = run("close-window", id, "-s", session)
				return rawPaneDone{window: id}
			}
			if _, err := run("get-window", id, "-s", session); err != nil {
				return rawPaneDone{window: id}
			}
		}
	}
}

// rawPaneClosed reads the file back once the editor's window is gone.
func (a *App) rawPaneClosed(msg rawPaneDone) {
	if a.raw == nil || a.raw.window != msg.window {
		return
	}
	a.raw.window = ""
	a.rawDone(ochazuke.Ctx{Runtime: a.r}, nil)
}

// attachRawPane is g r while the editor runs in a pane: focus it.
func (a *App) attachRawPane(c ochazuke.Ctx) ochazuke.Result {
	session, _, _ := a.tuiosHere()
	if _, err := a.tuios("focus-window", "-s", session, a.raw.window); err != nil {
		return ochazuke.Failed(errors.New("raw: tuios focus-window: " + err.Error()))
	}
	c.Info("RAW re-attached")
	return ochazuke.Done(nil)
}

// takeLater is the commands verbs left for the loop (an off-loop search,
// a RAW pane's watch), once.
func (a *App) takeLater() []tea.Cmd {
	out := a.later
	a.later = nil
	return out
}

func (a *App) registerRawPane() error {
	if a.tuios == nil {
		a.tuios = runTuios
	}
	if a.getenv == nil {
		a.getenv = os.Getenv
	}
	for _, s := range []ochazuke.Setting{
		{Name: settingRawPane, Type: ochazuke.SettingString, Default: rawPaneAuto, Enum: []string{rawPaneAuto, rawPaneForeground},
			Description: "Where g r (RAW) runs the editor: auto opens a tuios window when o-json runs in tuios (o-json keeps running; ctrl+a d in the editor comes back, g r goes to it), else the foreground; foreground always suspends o-json while the editor runs"},
		{Name: settingRawDetach, Type: ochazuke.SettingString, Default: rawDetachDefault,
			Description: "An ex command passed with -c to the editor in a tuios pane ({session} and {pane} are o-json's): by default ctrl+a d focuses o-json again (nvim, vim); empty passes nothing"},
		{Name: settingRawGrants, Type: ochazuke.SettingString, Default: "read,write",
			Description: "The tuios grants of RAW's editor window (new-window --grants); write lets ctrl+a d focus o-json"},
	} {
		if err := declareOwn(a.r, s); err != nil {
			return err
		}
	}
	return nil
}
