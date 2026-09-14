package fynelyrics

// word is a timed piece of a lyric line (a character or word).
// start < 0 means the piece carries no timing.
type word struct {
	text  string
	start float64
}

func locateActiveLine(starts []float64, cur float64) int {
	if len(starts) == 0 || cur < starts[0] {
		return -1
	}
	lo, hi := 0, len(starts)-1
	for lo < hi {
		mid := int(uint(lo+hi+1) >> 1)
		if starts[mid] <= cur {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func countActiveWords(words []word, cur float64) int {
	n := 0
	for _, w := range words {
		if w.start < 0 || w.start <= cur {
			n++
		} else {
			break
		}
	}
	return n
}

func nextBoundary(words []word, lineStarts []float64, lineIdx int, cur float64) (float64, bool) {
	for _, w := range words {
		if w.start >= 0 && w.start > cur {
			return w.start, true
		}
	}
	if lineIdx+1 < len(lineStarts) {
		return lineStarts[lineIdx+1], true
	}
	return 0, false
}
