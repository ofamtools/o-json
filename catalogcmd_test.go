package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The tool's own commands win over a file of the same name in the working
// directory: catalog prints the catalog JSON with no terminal, and ./catalog
// opens the file.
func TestCatalogCommandBeatsAFileNamedCatalog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_STATE_HOME", dir+"/state")
	t.Setenv("XDG_DATA_HOME", dir+"/data")
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.WriteFile("catalog", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if err := Main([]string{"catalog"}, strings.NewReader(""), &out, &errb); err != nil {
		t.Fatalf("catalog: %v\n%s", err, errb.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc["name"] != "o-json" {
		t.Fatalf("catalog answered %v: %.200s", err, out.String())
	}
}
