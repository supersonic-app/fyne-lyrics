package fynelyrics

import (
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestMain(m *testing.M) {
	test.NewApp()
	os.Exit(m.Run())
}

func TestSetLyricLinesUsesSegmentsAndStarts(t *testing.T) {
	l := NewLyricsViewer()
	l.SetLyricLines([]LyricLine{
		{Start: 0, Segments: []LyricSegment{{Text: "狂", Start: 0}, {Text: "人", Start: 0.36}}},
		{Start: 7.29, Segments: []LyricSegment{{Text: "词：佚名", Start: 7.29}}},
	}, true)
	if len(l.lineWords) != 2 {
		t.Fatalf("lineWords: got %d want 2", len(l.lineWords))
	}
	if got := len(l.lineWords[0]); got != 2 {
		t.Errorf("line0 words: got %d want 2", got)
	}
	if l.lineStarts[0] != 0 || l.lineStarts[1] != 7.29 {
		t.Errorf("lineStarts: got %v want [0, 7.29]", l.lineStarts)
	}
}

// SetLyrics treats each string as a single untimed segment (no tag parsing;
// that is the caller's responsibility via SetLyricLines).
func TestSetLyricsWrapsPlainLines(t *testing.T) {
	l := NewLyricsViewer()
	l.LineStarts = []float64{1, 2, 3}
	l.SetLyrics([]string{"a", "b", "c"}, true)
	if len(l.lineWords) != 3 {
		t.Fatalf("lineWords: got %d want 3", len(l.lineWords))
	}
	if got := len(l.lineWords[0]); got != 1 {
		t.Errorf("plain line should have a single segment, got %d", got)
	}
	if l.lineWords[0][0].text != "a" || l.lineWords[0][0].start != -1 {
		t.Errorf("plain segment = %+v, want {a -1}", l.lineWords[0][0])
	}
	if l.lineStarts[0] != 1 || l.lineStarts[2] != 3 {
		t.Errorf("expected provided lineStarts, got %v", l.lineStarts)
	}
}

// During playback a stale/out-of-order non-seek update must not scroll back;
// a seek may move backwards.
func TestNonSeekBackwardUpdateIgnored(t *testing.T) {
	l := NewLyricsViewer()
	l.SetLyricLines([]LyricLine{
		{Start: 0, Segments: []LyricSegment{{Text: "a", Start: 0}}},
		{Start: 10, Segments: []LyricSegment{{Text: "b", Start: 10}}},
		{Start: 20, Segments: []LyricSegment{{Text: "c", Start: 20}}},
	}, true)
	l.CreateRenderer()
	l.Resize(fyne.NewSize(400, 300))

	l.SetPlayTime(20, false)
	if l.currentLine != 3 {
		t.Fatalf("currentLine = %d, want 3", l.currentLine)
	}
	l.SetPlayTime(0, false) // stale backward sample during playback
	if l.currentLine != 3 {
		t.Errorf("non-seek backward update moved to line %d, want 3", l.currentLine)
	}
	l.SetPlayTime(0, true) // explicit seek may move backwards
	if l.currentLine != 1 {
		t.Errorf("seek moved to line %d, want 1", l.currentLine)
	}
}
