//go:build ignore

// gen-jobs-data writes the shared sample the job journeys and tutorials
// open (testdata/jobs): an API dump of 25 orders on one minified line, the
// same data pretty-printed with one trailing comma (a broken file), and a
// small package.json-like config. The data is deterministic (fixed seed)
// and obviously invented: names from short lists, emails at example.com,
// ids in sequence. Copied in shape from ochazuke's docs/vhs/gen-demo-csv.go.
//
//	go run docs/tapes/gen-jobs-data.go testdata/jobs
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const orders = 25

var (
	first    = []string{"Aiko", "Ben", "Chika", "Dev", "Emi", "Finn", "Gus", "Hana", "Ivo", "Juno", "Kai", "Lena", "Mio"}
	last     = []string{"Abe", "Brook", "Castle", "Dune", "Ember", "Fern", "Grove", "Hill", "Isle", "Jade"}
	statuses = []string{"paid", "paid", "shipped", "shipped", "refunded", "pending"}
	skus     = []string{"TEA-SEN", "TEA-GEN", "TEA-HOJ", "TEA-MAT", "CUP-01", "POT-02"}
	coupons  = []string{"WELCOME10", "AUTUMN5"}
)

// field is one key and value, so objects keep the order written here.
type field struct {
	k string
	v any
}

type object []field

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.k)
		v, err := json.Marshal(f.v)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func main() {
	dir := "testdata/jobs"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	rng := rand.New(rand.NewPCG(2026, 1030))
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var data []any
	for i := 1; i <= orders; i++ {
		f, l := first[rng.IntN(len(first))], last[rng.IntN(len(last))]
		customer := object{{"name", f + " " + l}}
		switch {
		case i%7 == 3: // no email at all
		case i%5 == 4:
			customer = append(customer, field{"email", nil})
		default:
			customer = append(customer, field{"email", strings.ToLower(f+"."+l) + "@example.com"})
		}
		var items []any
		total := 0
		for range 1 + rng.IntN(3) {
			qty, price := 1+rng.IntN(4), 4+rng.IntN(40)
			total += qty * price
			items = append(items, object{{"sku", skus[rng.IntN(len(skus))]}, {"qty", qty}, {"price", price}})
		}
		o := object{
			{"id", fmt.Sprintf("ORD-%04d", 1000+i)},
			{"status", statuses[rng.IntN(len(statuses))]},
			{"customer", customer},
			{"items", items},
			{"total", total},
			{"created_at", start.Add(time.Duration(i*7+rng.IntN(6)) * time.Hour).Format(time.RFC3339)},
		}
		if i%4 == 2 {
			o = append(o, field{"coupon", coupons[rng.IntN(len(coupons))]})
		}
		data = append(data, o)
	}
	dump := object{{"data", data}, {"page", object{{"next", nil}, {"total", orders}}}}
	min, err := json.Marshal(dump)
	check(err)
	write(dir, "orders.min.json", append(min, '\n'))

	// The broken file: the same data indented, with one trailing comma
	// after the first order's last item.
	var pretty bytes.Buffer
	check(json.Indent(&pretty, min, "", "  "))
	text := pretty.String()
	at := strings.Index(text, "\"price\"")
	at += strings.Index(text[at:], "\n")
	text = text[:at] + "," + text[at:]
	write(dir, "orders-broken.json", []byte(text+"\n"))

	cfg := object{
		{"name", "tea-shop"},
		{"version", "1.4.2"},
		{"private", true},
		{"scripts", object{{"build", "vite build"}, {"dev", "vite"}, {"test", "vitest run"}}},
		{"dependencies", object{{"react", "^18.3.1"}, {"react-dom", "^18.3.1"}}},
		{"devDependencies", object{{"vite", "^5.4.0"}, {"vitest", "^2.1.0"}}},
		{"engines", object{{"node", ">=20"}}},
	}
	c, err := json.MarshalIndent(cfg, "", "  ")
	check(err)
	write(dir, "package.json", append(c, '\n'))
}

func write(dir, name string, b []byte) {
	check(os.WriteFile(filepath.Join(dir, name), b, 0o644))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
