package golang

import (
	"reflect"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/catest"
)

const goBase = `package calc

// Calc adds things.
type Calc struct {
	Base int
}

// Add adds n.
func (c *Calc) Add(n int) int {
	return c.Base + n
}

func helper(n int) int {
	return n
}

// Old goes away.
func Old() {}
`

const goHead = `package calc

// Calc adds things.
type Calc struct {
	Base  int
	Scale int
}

// Add adds n, scaled.
func (c *Calc) Add(n int, scale int) int {
	return c.Base + helper(n)*scale
}

func helper(n int) int {
	return n
}

// Double is new.
// It calls Add.
func Double(c *Calc, n int) int {
	return c.Add(n, 2)
}
`

func TestGoSampleFiles(t *testing.T) {
	r := codeanalysis.Run([]codeanalysis.FilePair{catest.Pair("calc/calc.go", goBase, goHead)}, New("example.com/m"))

	catest.Methods(t, r, []string{
		"calc/calc.go::(*Calc).Add changed new 9-12 decl 10 old 8-11 decl 9",
		"calc/calc.go::Double added new 18-22 decl 20",
		"calc/calc.go::Old removed old 17-18 decl 18",
	})
	catest.Edges(t, r, []string{
		"calc/calc.go::Double -> calc/calc.go::(*Calc).Add",
	})
	if got, want := r.DeclLines["calc/calc.go"], []int{3, 9, 14, 18}; !reflect.DeepEqual(got, want) {
		t.Errorf("decl stops = %v, want %v", got, want)
	}

	var got []string
	for _, c := range r.ExportedChanges {
		got = append(got, c.Kind+" "+c.Name)
	}
	want := []string{
		"func_sig_changed (*calc.Calc).Add",
		"func_added calc.Double",
		"func_removed calc.Old",
		"field_added calc.Calc.Scale",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("exported changes:\n got  %q\n want %q", got, want)
	}
}

func TestGoReadsOnlyGoFiles(t *testing.T) {
	a := New("")
	for p, want := range map[string]bool{"a.go": true, "a.ts": false, "a.rb": false, "go.mod": false} {
		if a.Reads(p) != want {
			t.Errorf("Reads(%q) = %v", p, !want)
		}
	}
}
