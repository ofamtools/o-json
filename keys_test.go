package main

import "ofam.dev/ochazuke/keytest"

// Tests drive the model with the key names bindings use ("ctrl+c", "esc",
// "enter", "shift+tab", "alt+b", "space", "x") through ochazuke's keytest
// (exported in v0.51; this file was a copy of it before), which is the
// only place a Bubble Tea key message is spelled out.
var (
	Press = keytest.Press
	Type  = keytest.Type
	Paste = keytest.Paste
)
