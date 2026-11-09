package main

// RAW in a daemon pane (Design D, raw.pane auto): inside a pane
// (MTM_ENV=1 or TUIOS_ENV=1 with a session and a pane id) g r opens the editor in a
// new pane through mtm (pane.open) instead of suspending o-json: the pane runs the
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
// Every daemon call is an mtm verb (pane open, show, switch, close)
// through App.mtm (a runner tests replace); the command word is the
// raw.pane-command setting. The environment goes through App.getenv.

import (
	"encoding/json"
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
	settingRawCmd    = "raw.pane-command"

	rawPaneAuto       = "auto"
	rawPaneForeground = "foreground"

	// rawPanePoll is how often o-json asks tuios whether the editor's
	// window is still there.
	rawPanePoll = 300 * time.Millisecond

	// rawDetachDefault focuses o-json's pane again; {command} is
	// raw.pane-command as Vim string items.
	rawDetachDefault = `nnoremap <silent> <C-a>d <Cmd>call system([{command}, 'pane', 'switch', '--session', '{session}', '--window', '{pane}'])<CR>`
)

// mtmRunner runs mtm with args (the verb words after the command word) and
// returns its standard output.
type mtmRunner func(args ...string) ([]byte, error)

// paneCommand is raw.pane-command split into words (mtm by default).
func (a *App) paneCommand() []string {
	if w := strings.Fields(a.r.Settings().String(settingRawCmd)); len(w) > 0 {
		return w
	}
	return []string{"mtm"}
}

// runMtm runs the command word(s) of raw.pane-command with args.
func (a *App) runMtm(args ...string) ([]byte, error) {
	w := a.paneCommand()
	out, err := exec.Command(w[0], append(w[1:], args...)...).Output()
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
	session, pane = paneEnv(a.getenv, "SESSION"), paneEnv(a.getenv, "PANE_ID")
	return session, pane, paneEnv(a.getenv, "ENV") == "1" && session != "" && pane != ""
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
	args := []string{"pane", "open", "--session", session, "--name", "raw-" + appName()}
	if g := rawGrants(a.r.Settings().String(settingRawGrants)); g != nil {
		b, _ := json.Marshal(g)
		args = append(args, "--grants", string(b))
	}
	// The window's process is the editor under sh, which leaves a marker
	// file when the editor exits: a tuios window may outlive its process,
	// so the marker, not the window, says the edit is over.
	done := a.raw.file + ".done"
	os.Remove(done)
	cmd := []string{"sh", "-c", `"$@"; s=$?; : > "$0"; exit $s`, done, argv[0]}
	if d := strings.TrimSpace(a.r.Settings().String(settingRawDetach)); d != "" {
		var items []string
		for _, w := range a.paneCommand() {
			items = append(items, "'"+strings.ReplaceAll(w, "'", "''")+"'")
		}
		d = strings.NewReplacer("{command}", strings.Join(items, ", "), "{session}", session, "{pane}", pane).Replace(d)
		cmd = append(cmd, "-c", d)
	}
	cmd = append(cmd, argv[1:]...)
	b, _ := json.Marshal(cmd)
	args = append(args, "--command", string(b))
	out, err := a.mtm(args...)
	if err != nil {
		return ochazuke.Failed(errors.New("raw: mtm pane open: " + err.Error()))
	}
	var opened struct {
		WindowID string `json:"window_id"`
	}
	_ = json.Unmarshal(out, &opened)
	id := opened.WindowID
	if id == "" {
		return ochazuke.Failed(errors.New("raw: mtm pane open answered no window_id"))
	}
	a.raw.window = id
	a.later = append(a.later, a.watchRawPane(session, id, done))
	c.Info("RAW " + a.raw.path + " in a tuios pane · ctrl+a d there comes back · g r goes to it")
	return ochazuke.Done(nil)
}

// watchRawPane waits for the editor's end: its marker file (then the
// window is closed), or the window gone (closed by hand).
func (a *App) watchRawPane(session, id, done string) tea.Cmd {
	run := a.mtm
	return func() tea.Msg {
		for {
			time.Sleep(rawPanePoll)
			if _, err := os.Stat(done); err == nil {
				os.Remove(done)
				_, _ = run("pane", "close", "--session", session, "--window", id)
				return rawPaneDone{window: id}
			}
			if _, err := run("pane", "show", "--session", session, "--window", id); err != nil {
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
	if _, err := a.mtm("pane", "switch", "--session", session, "--window", a.raw.window); err != nil {
		return ochazuke.Failed(errors.New("raw: mtm pane switch: " + err.Error()))
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
	if a.mtm == nil {
		a.mtm = a.runMtm
	}
	if a.getenv == nil {
		a.getenv = os.Getenv
	}
	for _, s := range []ochazuke.Setting{
		{Name: settingRawPane, Type: ochazuke.SettingString, Default: rawPaneAuto, Enum: []string{rawPaneAuto, rawPaneForeground},
			Description: "Where g r (RAW) runs the editor: auto opens a pane (mtm pane open) when o-json runs in a daemon pane (o-json keeps running; ctrl+a d in the editor comes back, g r goes to it), else the foreground; foreground always suspends o-json while the editor runs"},
		{Name: settingRawDetach, Type: ochazuke.SettingString, Default: rawDetachDefault,
			Description: "An ex command passed with -c to the editor in the RAW pane ({command} is raw.pane-command as Vim strings; {session} and {pane} are o-json's): by default ctrl+a d focuses o-json again (nvim, vim); empty passes nothing"},
		{Name: settingRawGrants, Type: ochazuke.SettingString, Default: "read,write",
			Description: "The grants of RAW's editor pane, comma separated (pane open grants); write lets ctrl+a d focus o-json"},
		{Name: settingRawCmd, Type: ochazuke.SettingString, Default: "mtm",
			Description: "The command word RAW's pane calls run through, split on spaces: mtm, or for example `mtm --server default` to reach another daemon server"},
	} {
		if err := declareOwn(a.r, s); err != nil {
			return err
		}
	}
	return nil
}

// rawGrants splits the comma separated raw.pane-grants setting.
func rawGrants(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}
