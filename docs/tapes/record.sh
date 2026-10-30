#!/bin/sh
# Record the guide's gifs: docs/tapes/*.tape -> docs/img/*.gif.
# Builds o-json to /tmp/o-json-guide/bin and copies testdata/*.json to
# /tmp/o-json-guide/vhs-work; each tape runs the program there with a
# throwaway HOME and XDG dirs, so no user config or state is read or written.
# Run from anywhere:  docs/tapes/record.sh [NAME...]   (needs vhs on PATH)
set -e
cd "$(dirname "$0")/../.."
mkdir -p /tmp/o-json-guide/bin /tmp/o-json-guide/vhs-work docs/img
GOWORK=off GOFLAGS=-trimpath go build -o /tmp/o-json-guide/bin/o-json .
rm -f /tmp/o-json-guide/vhs-work/*
cp testdata/*.json /tmp/o-json-guide/vhs-work/
if [ $# -eq 0 ]; then set -- docs/tapes/*.tape; fi
for tape in "$@"; do
	case "$tape" in *.tape) ;; *) tape="docs/tapes/$tape.tape" ;; esac
	echo "== $tape"
	vhs "$tape"
done
ls -l docs/img
