# o-json's two builds: the shipped viewer, and the story tester
# (-tags monogatari: o-json story, never installed over o-json).
GO_ENV := GOWORK=off GOFLAGS=-trimpath

.PHONY: story-build check split-check

# The tester: bin/o-json-story story run|open|list|... (o-json story in its help).
story-build:
	$(GO_ENV) go build -tags monogatari -o bin/o-json-story .

# The gate, both builds: gofmt, vet and the race suite without the tag (the
# shipped binary and the untagged tests), then with it (the tester, the
# stories through host.Test); then the split.
check:
	test -z "$$(gofmt -l .)"
	$(GO_ENV) go vet ./...
	$(GO_ENV) go test -race ./...
	$(GO_ENV) go vet -tags monogatari ./...
	$(GO_ENV) go test -race -tags monogatari ./...
	$(MAKE) split-check

# A normal build links no monogatari; the tester does.
split-check:
	@mkdir -p bin
	$(GO_ENV) go build -o bin/o-json .
	@n=$$(go tool nm bin/o-json | grep -c monogatari); echo "o-json: $$n monogatari symbols"; test "$$n" -eq 0
	$(MAKE) story-build
	@n=$$(go tool nm bin/o-json-story | grep -c monogatari); echo "o-json-story: $$n monogatari symbols"; test "$$n" -gt 0

# Portable tutorial frames. Override GO_ENV to build against a local monogatari.
# The story command has no --data flag; its fixtures own temporary documents.
OUT ?= /tmp/html-tutorials/exports
TUTORIAL_TMP ?= /tmp/html-tutorials/o-json
.PHONY: tutorials-export
tutorials-export: story-build
	@mkdir -p "$(TUTORIAL_TMP)/home" "$(TUTORIAL_TMP)/config" "$(TUTORIAL_TMP)/state" "$(TUTORIAL_TMP)/data" "$(TUTORIAL_TMP)/tmp"
	env -u TUIOS_ENV -u TUIOS_SESSION -u TUIOS_PANE_ID HOME="$(TUTORIAL_TMP)/home" XDG_CONFIG_HOME="$(TUTORIAL_TMP)/config" XDG_STATE_HOME="$(TUTORIAL_TMP)/state" XDG_DATA_HOME="$(TUTORIAL_TMP)/data" TMPDIR="$(TUTORIAL_TMP)/tmp" bin/o-json-story story export tutorials --out "$(OUT)" --format json
