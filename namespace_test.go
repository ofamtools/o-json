package main

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

func TestCatalogDeclaresSingularNamespace(t *testing.T) {
	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(run(t, "", "catalog")), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Namespace != "json" || ochazuke.DefaultNamespace != "json" {
		t.Fatalf("catalog namespace %q, default %q; want json", doc.Namespace, ochazuke.DefaultNamespace)
	}
	if got, e := ocli.CatalogNamespace(doc, "o-json"); e != nil || got != "json" {
		t.Fatal(got, e)
	}
}
