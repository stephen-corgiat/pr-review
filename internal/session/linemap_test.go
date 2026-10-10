package session

import "testing"

func TestLineMap(t *testing.T) {
	// Line 8 edited; 3 lines inserted after 15; 3 inserted after 19.
	lm := LineMap{{8, 1, 8, 1}, {15, 0, 16, 3}, {19, 0, 23, 3}}
	for _, c := range []struct {
		l, want int
		edited  bool
	}{{1, 1, false}, {7, 7, false}, {8, 8, true}, {9, 9, false}, {15, 15, false}, {16, 19, false}, {19, 22, false}, {20, 26, false}} {
		if n, e := lm.Line(c.l); n != c.want || e != c.edited {
			t.Errorf("Line(%d) = %d,%v want %d,%v", c.l, n, e, c.want, c.edited)
		}
	}
	for _, c := range []struct {
		f, t, nf, nt int
		edited       bool
	}{
		{4, 6, 4, 6, false},    // untouched
		{16, 21, 19, 27, true}, // an insertion after 19 lands inside
		{16, 19, 19, 22, false},
		{7, 9, 7, 9, true}, // spans the edited line
		{14, 15, 14, 15, false},
	} {
		nf, nt, e := lm.Range(c.f, c.t)
		if nf != c.nf || nt != c.nt || e != c.edited {
			t.Errorf("Range(%d,%d) = %d,%d,%v want %d,%d,%v", c.f, c.t, nf, nt, e, c.nf, c.nt, c.edited)
		}
	}
	// Deleting lines 3-4 (to nothing after new line 2).
	del := LineMap{{3, 2, 2, 0}}
	if n, e := del.Line(5); n != 3 || e {
		t.Errorf("after a deletion: %d %v", n, e)
	}
	if n, e := del.Line(4); !e || n != 2 {
		t.Errorf("a deleted line: %d %v", n, e)
	}
}
