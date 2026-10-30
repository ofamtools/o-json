#!/bin/sh
# Regenerate the cheat sheet at the end of docs/GUIDE.md from the key rows
# (bindings.json and the ochazuke rows o-json runs with). The generator is
# cheatsheet_test.go, so it reads the same runtime the viewer builds and
# TestCheatSheet (part of go test ./...) fails when the committed sheet
# drifts from the bindings.
set -e
cd "$(dirname "$0")/.."
UPDATE_CHEATSHEET=1 GOWORK=off GOFLAGS=-trimpath go test -count=1 -run '^TestCheatSheet$' .
