package fynelyrics

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func collectTexts(o fyne.CanvasObject, out *[]*canvas.Text) {
	if t, ok := o.(*canvas.Text); ok {
		*out = append(*out, t)
		return
	}
	if w, ok := o.(fyne.Widget); ok {
		for _, c := range test.WidgetRenderer(w).Objects() {
			collectTexts(c, out)
		}
		return
	}
	if c, ok := o.(*fyne.Container); ok {
		for _, ch := range c.Objects {
			collectTexts(ch, out)
		}
	}
}

func TestKaraokeLineRendersOnOneRow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	l := NewLyricsViewer()
	l.ActiveLyricPosition = ActiveLyricPositionUpperMiddle
	l.SetLyricLines([]LyricLine{
		{Start: 0, Segments: []LyricSegment{
			{Text: "狂", Start: 0}, {Text: "人", Start: 0.36}, {Text: "日", Start: 0.72},
			{Text: "记", Start: 1.09}, {Text: " ", Start: 1.45}, {Text: "(", Start: 1.82},
			{Text: "Live", Start: 2.18},
		}},
		{Start: 7.29, Segments: []LyricSegment{
			{Text: "词", Start: 7.29}, {Text: "：", Start: 9.11},
			{Text: "林", Start: 10.93}, {Text: "夕", Start: 12.75},
		}},
	}, true)
	l.SetPlayTime(0, true)
	l.Resize(fyne.NewSize(640, 480))

	ll1 := l.vbox.Objects[1].(*lyricLine)
	if len(ll1.words) < 2 {
		t.Fatalf("expected multi-word line, got %d", len(ll1.words))
	}
	var texts []*canvas.Text
	collectTexts(ll1, &texts)
	if len(texts) != 2 {
		t.Fatalf("expected base + active texts, got %d", len(texts))
	}
	base, active := texts[0], texts[1]
	if base.Text != ll1.fullText() {
		t.Errorf("base text %q != full line %q", base.Text, ll1.fullText())
	}
	// both layers must use the exact same string so their shaping (and thus the
	// baseline of every glyph) matches and there is no ghosting
	if active.Text != base.Text {
		t.Errorf("active text %q must equal base text %q to avoid ghosting", active.Text, base.Text)
	}
	if base.Position().Y != active.Position().Y || base.Position().X != active.Position().X {
		t.Errorf("base %v and active %v not overlaid", base.Position(), active.Position())
	}
	if ll1.activeClip == nil {
		t.Errorf("expected a clipping overlay for a multi-word line")
	}
}

// Plain lines (no inline tags) must keep the original rendering: a single
// wrapping RichText using the active/inactive line color.
func TestPlainLineKeepsRichTextAndActiveColor(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	l := NewLyricsViewer()
	lines := []string{"a plain lyric line without any tags", "another plain line"}
	l.SetLyrics(lines, true)
	l.SetCurrentLine(1) // line-driven mode: line 1 becomes active
	l.Resize(fyne.NewSize(400, 300))

	ll := l.vbox.Objects[1].(*lyricLine)
	if ll.isKaraoke() {
		t.Fatalf("a line without inline tags must not be karaoke")
	}
	rt, ok := ll.content().(*widget.RichText)
	if !ok {
		t.Fatalf("plain line content = %T, want *widget.RichText", ll.content())
	}
	if rt.Wrapping != fyne.TextWrapWord {
		t.Errorf("plain line Wrapping = %v, want TextWrapWord", rt.Wrapping)
	}
	seg, ok := rt.Segments[0].(*widget.TextSegment)
	if !ok {
		t.Fatalf("plain line segment = %T, want *widget.TextSegment", rt.Segments[0])
	}
	if seg.Style.ColorName != ll.ColorName {
		t.Errorf("active plain line color = %v, want %v", seg.Style.ColorName, ll.ColorName)
	}
}
