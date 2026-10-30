package main

import "testing"

// The diff's patches turn the old value into the new one, by path; each
// case also names how many patches it takes.
func TestRawDiffPatchesRoundTrip(t *testing.T) {
	cases := []struct {
		name, old, new string
		patches        int
	}{
		{"scalar", `{"a":1,"b":2}`, `{"a":1,"b":3}`, 1},
		{"nested scalar", `{"a":{"x":[1,2,{"y":"z"}]}}`, `{"a":{"x":[1,2,{"y":"Z"}]}}`, 1},
		{"add key after", `{"a":1,"c":3}`, `{"a":1,"b":2,"c":3}`, 1},
		{"add key first", `{"b":1}`, `{"a":0,"b":1}`, 1},
		{"add to empty", `{}`, `{"a":1,"b":2}`, 2},
		{"delete key", `{"a":1,"b":2,"c":3}`, `{"a":1,"c":3}`, 1},
		{"rename", `{"a":1,"b":2,"c":3}`, `{"a":1,"B":2,"c":3}`, 2},
		{"reorder", `{"a":1,"b":2}`, `{"b":2,"a":1}`, 1},
		{"array insert front", `[1,2,3]`, `[0,1,2,3]`, 1},
		{"array delete middle", `[1,2,3,4]`, `[1,4]`, 2},
		{"array change and grow", `[{"n":1},{"n":2}]`, `[{"n":1},{"n":5},{"n":6}]`, 2},
		{"kind change", `{"a":[1]}`, `{"a":{"b":1}}`, 1},
		{"document", `1`, `"one"`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old, err := Build([]byte(c.old))
			if err != nil {
				t.Fatal(err)
			}
			new, err := Build([]byte(c.new))
			if err != nil {
				t.Fatal(err)
			}
			ps := diffPatches(old, new, ".")
			got, orphans := applyPatches(old, ps)
			if len(orphans) > 0 || Compact(got, 0) != Compact(new, 0) {
				t.Fatalf("patches %+v: got %s orphans %v, want %s", ps, Compact(got, 0), orphans, c.new)
			}
			if len(ps) != c.patches {
				t.Fatalf("%d patches %+v, want %d", len(ps), ps, c.patches)
			}
		})
	}
}
