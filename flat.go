package main

// The FLAT view's look: each path step of a `path = value` line drawn in
// the tone of its nesting depth, the outer steps dimmed so the line's own
// field (its innermost step) stands out, and array indexes padded to the
// width of their array's last index so the lines of one array line up.
//
// The look is display only. A line's text (dataLine.text: y y, the list
// filter, export) and its node (y p, y v, enter, jq paths) stay the
// unpadded path; the drawn line is dataLine.shown, its colours
// dataLine.tones beside the text (ochazuke tone spans, never escapes in
// the text).
//
// The depth tones are theme data: the tone rows flat.depth.1 ..
// flat.depth.N (the innermost step) and flat.depth.1.dim ..
// flat.depth.N.dim (an outer step), declared here for every built-in
// theme (light and dark sides; mono draws the dim attribute alone) and
// recoloured, or extended with flat.depth.7 and on, by a themes.json
// theme. Depth d takes row ((d-1) mod N)+1, so the list cycles.
//
// Two settings turn the look off: flat.depth-colors (default on) and
// flat.pad-indexes (default on).

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke"
)

const (
	settingFlatDepthColors = "flat.depth-colors"
	settingFlatPadIndexes  = "flat.pad-indexes"
)

// flatDepthTone is the tone row prefix of the depth tones.
const flatDepthTone = "flat.depth."

// flatDepthHues are the built-in depth tones (light, dark), outermost
// first, in the order a diff tool's nesting colours run.
var flatDepthHues = [][2]string{
	{"#1F6FB2", "#7CB7E8"}, // blue
	{"#2E7D32", "#8BC48A"}, // green
	{"#8B5A26", "#E7B673"}, // amber
	{"#7952A3", "#C7A2ED"}, // violet
	{"#166C78", "#74CDD4"}, // teal
	{"#A44049", "#F2949A"}, // rose
}

func declareFlatSettings(r *ochazuke.Runtime) error {
	if err := declareOwn(r, ochazuke.Setting{
		Name: settingFlatDepthColors, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The FLAT view draws each path step in the colour of its nesting depth (the tone rows flat.depth.1 to flat.depth.N, cycling; a theme recolours or adds them) and dims every step but the line's innermost; off draws the lines plain",
	}); err != nil {
		return err
	}
	return declareOwn(r, ochazuke.Setting{
		Name: settingFlatPadIndexes, Type: ochazuke.SettingBoolean, Default: true,
		Description: "The FLAT view pads array indexes with spaces, right-aligned to the width of their array's last index ([  1] in an array whose last index is 234); yanks, search and jq paths keep the unpadded path. Off draws indexes as they are",
	})
}

// declareFlatTones declares the built-in depth tone rows.
func (a *App) declareFlatTones() {
	for i, h := range flatDepthHues {
		hue := ochazuke.SchemeColor{Light: h[0], Dark: h[1]}
		name := flatDepthTone + strconv.Itoa(i+1)
		a.m.Appearance.SetTone(name, ochazuke.ToneStyle{Fg: hue})
		a.m.Appearance.SetTone(name+".dim", ochazuke.ToneStyle{Fg: hue, Dim: true})
	}
}

// flatLook is how the FLAT view draws its lines: depths depth tones (0
// for none: plain) and padded indexes.
type flatLook struct {
	depths int
	pad    bool
}

// flatLook is the look the settings and the theme ask for now.
func (a *App) flatLook() flatLook {
	look := flatLook{pad: a.r.Settings().Bool(settingFlatPadIndexes)}
	if a.r.Settings().Bool(settingFlatDepthColors) {
		look.depths = a.flatDepths()
	}
	return look
}

// flatDepths is how many depth tones there are: the rows flat.depth.1,
// flat.depth.2, ... with none missing, from the application's rows and
// the current theme's themes.json entries (a theme may add rows).
func (a *App) flatDepths() int {
	app := a.m.Appearance
	rows := app.AppTones.Rows()
	has := func(name string) bool {
		if _, ok := rows[name]; ok {
			return true
		}
		for _, theme := range []string{app.Theme, "adaptive"} {
			if s, ok := app.Schemes.Get(theme); ok {
				if _, ok := s.Tones[name]; ok {
					return true
				}
			}
		}
		return false
	}
	n := 0
	for has(flatDepthTone + strconv.Itoa(n+1)) {
		n++
	}
	return n
}

// flatPath is a leaf's path as the FLAT view draws it, and the tones of
// its steps (cell 0, rune offsets): step d (1 the outermost) in depth
// tone ((d-1) mod depths)+1, dimmed unless it is the innermost. Indexes
// are padded to their array's last index's width when look.pad is set.
func flatPath(n *Node, look flatLook) (string, []ochazuke.ToneSpan) {
	var steps []string
	for at := n; at.Parent != nil; at = at.Parent {
		s := step(at)
		if look.pad && at.Parent.Kind == KArray {
			w := len(strconv.Itoa(len(at.Parent.Children) - 1))
			s = "[" + strings.Repeat(" ", w-len(strconv.Itoa(at.Index))) + strconv.Itoa(at.Index) + "]"
		}
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return ".", nil
	}
	var b strings.Builder
	var tones []ochazuke.ToneSpan
	at := 0
	for d, i := 1, len(steps)-1; i >= 0; d, i = d+1, i-1 {
		b.WriteString(steps[i])
		end := at + utf8.RuneCountInString(steps[i])
		if look.depths > 0 {
			tone := flatDepthTone + strconv.Itoa((d-1)%look.depths+1)
			if i > 0 {
				tone += ".dim"
			}
			tones = append(tones, ochazuke.SpanTone(0, at, end, tone))
		}
		at = end
	}
	return b.String(), tones
}
