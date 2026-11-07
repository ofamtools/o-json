package main

import "ofam.dev/ochazuke"

// declarePickers declares the tool's noun pickers (ochazuke.PickerDef): the
// catalog lists them and a host mounts them under the tool's prefix. Rows come
// from each noun's list verb and a pick runs the noun's own open or get, as
// the derived pickers do; declaring them gives each an About line.
func declarePickers(r *ochazuke.Runtime) error {
	for _, d := range []ochazuke.PickerDef{
		{Name: "doc", Noun: "doc", About: "The open document"},
		{Name: "node", Noun: "node", About: "The nodes of the open document"},
		{Name: "modes", Verb: "viewer.mode", About: "The mode names; pick one to go to it"},
	} {
		if err := r.AddPicker(d); err != nil {
			return err
		}
	}
	return nil
}
