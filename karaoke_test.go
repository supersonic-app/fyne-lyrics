package fynelyrics

import "testing"

func TestLocateActiveLine(t *testing.T) {
	starts := []float64{0, 7.29, 14.58, 21.88}
	cases := []struct {
		cur  float64
		want int
	}{
		{-1, -1},
		{0, 0},
		{7.0, 0},
		{7.29, 1},
		{20.0, 2},
		{100, 3},
	}
	for _, c := range cases {
		got := locateActiveLine(starts, c.cur)
		if got != c.want {
			t.Errorf("cur=%v: got %d want %d", c.cur, got, c.want)
		}
	}
}

func TestCountActiveWords(t *testing.T) {
	words := []word{{"狂", 0}, {"人", 0.36}, {"日", 0.72}}
	if got := countActiveWords(words, 0); got != 1 {
		t.Errorf("cur=0: got %d want 1", got)
	}
	if got := countActiveWords(words, 0.36); got != 2 {
		t.Errorf("cur=0.36: got %d want 2", got)
	}
	if got := countActiveWords(words, 0.72); got != 3 {
		t.Errorf("cur=0.72: got %d want 3", got)
	}
	if got := countActiveWords(words, 2); got != 3 {
		t.Errorf("cur=2: got %d want 3", got)
	}
	untimed := []word{{"前缀", -1}, {"字", 1.0}}
	if got := countActiveWords(untimed, 0.5); got != 1 {
		t.Errorf("untimed prefix cur=0.5: got %d want 1", got)
	}
}

func TestNextBoundary(t *testing.T) {
	words := []word{{"狂", 0}, {"人", 0.36}, {"日", 0.72}}
	starts := []float64{0, 7.29}
	if b, ok := nextBoundary(words, starts, 0, 0); !ok || b != 0.36 {
		t.Errorf("cur=0: got (%v,%v) want (0.36,true)", b, ok)
	}
	if b, ok := nextBoundary(words, starts, 0, 0.36); !ok || b != 0.72 {
		t.Errorf("cur=0.36: got (%v,%v) want (0.72,true)", b, ok)
	}
	if b, ok := nextBoundary(words, starts, 0, 0.72); !ok || b != 7.29 {
		t.Errorf("cur=0.72 (line end): got (%v,%v) want (7.29,true)", b, ok)
	}
	if _, ok := nextBoundary(words, starts, 1, 100); ok {
		t.Errorf("no boundary expected at end")
	}
}
