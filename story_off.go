//go:build !monogatari

package main

import "github.com/unixsuperhero/oden"

// storyCommands is empty in a normal build: only the tester (-tags
// monogatari, story.go) has o-json story, so the shipped binary links no
// monogatari.
func storyCommands() []oden.Command { return nil }
