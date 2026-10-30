#!/bin/sh
# test-chunks.sh OUT [N] [go test flags...]
# Runs o-json's top-level tests in N chunks (default 4), one go test at a
# time, so each stays under go test's default 10m timeout: go test -json
# per chunk into OUT/chunk.a?.json, each exit in OUT/status, then
# OUT/done. Run from the repo root. Example (the race gate in chunks):
#   scripts/test-chunks.sh /tmp/o-json-race 4 -race
#   python3 scripts/slowest.py /tmp/o-json-race 15
out=${1:?usage: test-chunks.sh OUT [N] [go test flags...]}
n=${2:-4}
shift; [ $# -gt 0 ] && shift
mkdir -p "$out"
rm -f "$out"/chunk.* "$out"/status "$out"/done "$out"/tests.txt
GOWORK=off GOFLAGS=-trimpath go test -list '.*' . | grep '^Test' > "$out/tests.txt"
per=$(( ($(wc -l < "$out/tests.txt") + n - 1) / n ))
split -l "$per" "$out/tests.txt" "$out/chunk."
for c in "$out"/chunk.a?; do
  re="^($(paste -sd'|' "$c"))\$"
  GOWORK=off GOFLAGS=-trimpath nice go test "$@" -count=1 -json -run "$re" . > "$c.json" 2>&1
  echo "$c exit $?" >> "$out/status"
done
touch "$out/done"
