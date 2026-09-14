package fynelyrics

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestLineTransitionKeepsPerWordProgress(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	l := NewLyricsViewer()
	l.ActiveLyricPosition = ActiveLyricPositionUpperMiddle
	// old line: 3 words, all sung at boundary; new line: only 2 words, and its
	// second word is >1s away so schedulePredictive schedules nothing
	l.SetLyricLines([]LyricLine{
		{Start: 21.88, Segments: []LyricSegment{
			{Text: "愿", Start: 21.88}, {Text: "望", Start: 22.41}, {Text: "多", Start: 24.17},
		}},
		{Start: 25.14, Segments: []LyricSegment{
			{Text: "年", Start: 25.14}, {Text: "少", Start: 30.00},
		}},
	}, true)
	l.CreateRenderer()
	l.Resize(fyne.NewSize(640, 480))

	// fully sing the old line, then cross into the new line at its start
	l.advanceTo(25.14)

	if l.currentLine != 2 {
		t.Fatalf("currentLine = %d, want 2", l.currentLine)
	}
	ll := l.vbox.Objects[2].(*lyricLine)
	if ll.activeWordIdx != 0 {
		t.Errorf("line 2 activeWordIdx = %d, want 0 (only first word sung at line start)", ll.activeWordIdx)
	}
	if ll.activeWordIdx == len(ll.words)-1 {
		t.Errorf("line 2 fully highlighted (activeWordIdx=%d of %d words); want per-word progress", ll.activeWordIdx, len(ll.words))
	}
}
