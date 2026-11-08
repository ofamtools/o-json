package main

import "ofam.dev/ochazuke"

// Namespace is the singular namespace o-json declares (lexicon plan): its
// operations are json.<noun>.<verb> and a host mounts its catalog under it.
const Namespace = "json"

func init() {
	if err := ochazuke.DeclareNamespace(Namespace); err != nil {
		panic(err)
	}
}
