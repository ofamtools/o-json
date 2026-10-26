//go:build race

package main

// raceOn scales the large-document tests down under the race detector,
// which runs them several times slower.
const raceOn = true
