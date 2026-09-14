# Inline Karaoke (Word-by-Word Highlighting) Support — Design Document

Date: 2026-09-10
Status: Implemented

## Background and Goals

fyne-lyrics is a Fyne lyric-display widget supporting synced and unsynced modes.
Originally it was driven from the outside at "line" granularity via
`NextLine()` / `SetCurrentLine(n)`.

It needs to support word-by-word lyric formats with inline tags, for example:

```lrc
[00:00.00]<00:00.00>床<00:00.40>前<00:00.80>明<00:01.20>月<00:01.60>光<00:02.00> <00:02.40>(Moonlight)<00:03.00>...
```

That is, each line of text embeds `<MM:SS.CC>` tags indicating the start time
(hundredths of a second with 2 digits, or milliseconds with 3 digits) of the
following character/word.
Goal: within the active line, light up characters progressively as playback
advances (karaoke effect).

### Relationship with the downstream consumer (supersonic)

**Parsing happens outside this project (supersonic).** supersonic's lyric layer
parses every source into a canonical model (line `[...]` and inline per-word
`<...>` timestamps) and hands it to fyne-lyrics. fyne-lyrics does **not** parse
raw text; it only receives timed data and does the time-sync and rendering.

- lrclib path: parses the synced LRC (with inline `<...>` tags) into line starts
  and per-word segments.
- subsonic structured path: each line has `Text` + `Start` and no per-word
  segments (the whole line is one segment).

So the widget's input is a "canonical lyric model" that may carry per-word
timing, or only line timing.

## Design Principles

- **Fully backward compatible**: existing `SetLyrics`/`SetCurrentLine`/`NextLine`/
  `OnLyricTapped` behavior is unchanged.
- **Parsing and rendering are separated**: timestamp parsing belongs to the
  caller; time-sync and rendering belong to this widget.
- **Mode is determined by whether `SetPlayTime` is called**:
  - Never called → keep the existing `NextLine`/`SetCurrentLine` behavior.
  - Once called → enter "time-driven mode", where the widget advances the line,
    lights characters, and scrolls on its own for each `SetPlayTime`.
- **Performance**: the push supplies a coarse time baseline; a short-range
  predictive timer refines word-boundary precision. Redraw only on state change.

## New Public API

### 1. Play-time input (push mode)

```go
// Called by the consumer on every play-time update. curTime is the current
// play time in seconds; seeked indicates whether this update came from a seek.
// Until SetPlayTime is called the widget keeps the existing NextLine /
// SetCurrentLine behavior; once called it enters time-driven mode.
// Must be called on the UI thread: it mutates widget state and starts/stops
// animations.
func (l *LyricsViewer) SetPlayTime(curTime float64, seeked bool)
```

Push rather than pull (a function handle), because:

- supersonic already has an `OnPlayTimeUpdate` polling loop (normally 250ms,
  100ms with waveform); the consumer just forwards to a callback it already
  has, with zero extra burden.
- The call cadence is set by the supplier's polling rate (~250ms), not
  per-word high frequency, so there is no performance concern.
- The widget maintains only one **short-range predictive timer** for
  word-boundary precision; no long-lived goroutine and no self-built scheduler.

### 2. Lyric input (canonical model, parsed by the caller)

```go
type LyricSegment struct {
    Text  string
    Start float64 // seconds; < 0 = untimed
}
type LyricLine struct {
    Start    float64
    Segments []LyricSegment
}
// Pass an already-parsed model. The caller parses the source format (LRC etc.).
func (l *LyricsViewer) SetLyricLines(lines []LyricLine, synced bool)
```

### 3. Compatibility: legacy SetLyrics + LineStarts

```go
// Plain lyrics: each line is one untimed segment; starts may come from LineStarts.
func (l *LyricsViewer) SetLyrics(lines []string, synced bool)
LineStarts []float64 // used only by the legacy SetLyrics path
```

## Input Model and Responsibility Boundary

- **Parsing (line + inline)**: the caller (supersonic). The time format is parsed
  in exactly one place, so precision is consistent.
- **Time-sync + rendering**: this widget. Given `LyricLine{Start, Segments}` and
  the current play position, it locates the active line/word, scrolls,
  highlights, and uses the predictive timer to refine word boundaries between
  pushes.
- Internally each `LyricSegment` maps to an internal `word{text, start}`;
  `start < 0` means untimed.
- A line without `Segments` (plain lyrics) is a single untimed segment and uses
  the fallback rendering.

## Rendering (Time-Driven Mode)

### Key constraint: font fallback and baselines

Under a real GL driver (especially macOS with supersonic's Fyne fork), **each
independent text object performs its own font fallback**. When a line is split
into multiple text objects (one per character/word), the same glyph may resolve
to different fallback fonts, so **baselines differ and characters sit at
different heights**.

Note: `RenderedTextSize` reports identical baselines for all characters (e.g.
19.23 for all), but the actual rendering differs — the **measured value is
unreliable**. So "manually aligning multiple text objects by their measured
baseline" cannot fix this.

**The only reliable approach is a single shaping run**: when laying out a
*single string*, go-text aligns all fallback runs to one baseline; split across
multiple strings, each is on its own.

### Implementation: whole-line base + clipped same-string overlay

`lyricLine` no longer creates one text segment per word. Instead:

- `baseText`: a single `canvas.Text` of the whole line (all words concatenated),
  in `InactiveLyricColor`.
- `activeText`: another `canvas.Text` of **exactly the same whole-line string**,
  in `ActiveLyricColor`.
- `activeText` is wrapped in a clipping widget `lyricClip` whose width equals the
  **pixel width of the sung prefix**.

How it works:

- Both layers use the identical string → shaping is per-glyph identical → glyphs
  coincide pixel-for-pixel, **no ghosting**.
- Clipping reveals only the sung prefix → word-by-word highlighting.
- The clip width is measured with a `canvas.Text` of the prefix (the same
  measurement path as rendering), so the boundary never cuts into the next glyph.

**Cross-version clipping compatibility** (critical):

| Fyne version | Clipping trigger |
|---|---|
| Fyne 2.5.x | object implements `fyne.Scrollable` |
| supersonic's Fyne fork | renderer implements `IsClip()` |

`lyricClip` implements **both**, so clipping works on both versions.
In synced mode `scroll.Direction = ScrollNone` (scrolling is already disabled),
so the `Scrollable` marker does not affect scrolling; unsynced mode has no inline
tags, produces no multi-word lines, and therefore never uses clipping.

### Line window derivation

A line's active time window is `[this line's Start, next line's Start)` (the
`Start` comes from `LyricLine.Start`). A line with no timing is shown statically
in InactiveLyricColor.

### Active-line scrolling

Reuses the existing `offsetForLine` logic:

- Normal line change: animate the scroll to the exact target offset
  (`offsetForLine(currentLine)`), centered.
- `seeked=true` (seek): **jump instantly** to the exact offset, no animation.
- Word-by-word lighting only updates the overlay's clip width; it does not
  trigger line scrolling.

## Internal Advancement: Push + Predictive Timer

In time-driven mode, `SetPlayTime` (~every 250ms) provides a coarse time
baseline, and a **short-range predictive timer** refines word-boundary precision
so a word lights exactly at its `start` instead of waiting for the next push.

The predictive timer is implemented with **`fyne.Animation`** (not
`time.AfterFunc`): Fyne 2.5.3 has no `fyne.Do`, whereas a `fyne.Animation`
callback runs on the UI thread and stops automatically when the widget is not
visible — no goroutine and no thread-scheduling code. Each `SetPlayTime` cancels
and reschedules this animation.

**On each `SetPlayTime(curTime, seeked)`:**

1. Record `lastCurTime = curTime`, `lastPushAt = now` (wall clock); cancel the
   pending predictive animation.
2. Locate the current line + `activeWordIdx` from `curTime` (`seeked=true` forces
   a full reposition; otherwise incremental).
3. Find the next word/line boundary `T_next` (the nearest one > `curTime`).
4. If `T_next - curTime <= predictiveHorizon` (1s), schedule
   `fyne.Animation(T_next - curTime)`; otherwise don't (wait for the next push).

**When the predictive animation completes (callback on the UI thread):**

1. `elapsed = now - lastPushAt`.
2. **Stall detection**: if `elapsed > stallThreshold` (600ms, ~2.5× the 250ms
   poll) → treat as paused/stopped, cancel further scheduling and return (the
   next `SetPlayTime` resumes).
3. Otherwise: light the words in the current line with `start <= T_next`
   (advance `activeWordIdx`), switching lines/scrolling if needed.
4. Find the next boundary and schedule again per step 4.

**Pause safety**: supersonic stops pushing `OnPlayTimeUpdate` while paused
(confirmed: `OnPaused → stopPollTimePos`). Pure push freezes naturally; the
predictive timer stays pause-safe via stall detection — during a pause it
advances at most about `stallThreshold` (~1 word) incorrectly, and the first push
after resume corrects it immediately from the real `curTime`.

**Bounds**:

- `predictiveHorizon` limits a single prediction span, so an instrumental section
  does not schedule a far-future timer making a long wall-clock assumption.
- Variable-rate playback (rate≠1) makes the prediction drift; the next push
  corrects it, which is acceptable.
- `SetLyrics` / widget destruction / `seeked=true` cancel the pending predictive
  animation.

### Rendering cost

On each position change, the text and color of the two whole-line
`canvas.Text` objects (base/active) are rebuilt. Each line has only two text
objects, so the cost is negligible; no per-segment incremental coloring.

## Highlight Granularity (v1 scope)

- **Word-by-word color change** (default, v1): when a word reaches its `start`,
  the clip width advances past it and that word switches to the Active color.
- **Smooth gradient** (optional enhancement, not implemented): interpolating the
  color between words by progress is what mainstream apps do, but it requires
  per-frame redraw and is expensive. YAGNI — left for later.

## Compatibility and Integration

- All legacy APIs (`SetLyrics`/`SetCurrentLine`/`NextLine`/`OnLyricTapped`) are
  preserved; `SetLyrics` treats each line as one untimed segment.
- When `SetPlayTime` is never called, behavior is identical to before.
- Plain lyrics (no per-word segments) still display in time-driven mode, just
  without word-by-word effect.
- supersonic integration: parse the lyrics into `[]fynelyrics.LyricLine`, call
  `SetLyricLines`, and forward the play position to `SetPlayTime`:

```go
pm.OnPlayTimeUpdate(func(cur, _ float64, seeked bool) {
    fyne.Do(func() { lyricsViewer.SetPlayTime(cur, seeked) })
})
```

The logic in its `UpdatePlayPos`/`OnSeeked` that drives
`NextLine`/`SetCurrentLine` is disabled and replaced by forwarding to
`SetPlayTime`.

## Error and Edge Handling

- A lyric line with empty text / empty `Segments` → an empty line is shown.
- A negative or invalid line/segment time → that line/segment is treated as
  untimed and shown statically.
- `LineStarts` (legacy path) length mismatching `lines` → fall back to the line
  start / static display.
- `SetPlayTime` with negative or clearly invalid `curTime` → treat as 0, i.e. the
  earliest position.
- `advanceTo` skips safely when `currentLine == 0` (no line entered yet), no
  out-of-range access.
- Repeated `SetLyricLines`/`SetLyrics` → full reset (current line back to the
  first, `activeWordIdx` cleared, `timeDriven` reset) and cancel the pending
  predictive animation.
- Multiple `SetPlayTime` calls within one play-time update → idempotent; redraw
  only when the position changes.

## Test Strategy

- **Pure logic** (`karaoke_test.go`): `locateActiveLine` line location,
  `countActiveWords` count of sung words, `nextBoundary` next boundary.
- **Viewer** (`lyricsviewer_test.go`): `SetLyricLines` uses `Segments`/`Start`;
  `SetLyrics` wraps plain lines as single segments and uses `LineStarts`.
- **Rendering** (`lyricline_test.go`): a multi-word line's base and active layers
  use the identical string, share the same position, and a clipping overlay
  exists; a plain line uses a RichText with the correct active color.
- **Line transition** (`line_transition_test.go`): when crossing a line boundary,
  the new line's `activeWordIdx` is computed from the target line's words, so the
  line does not "flash fully lit" first.
- **Compatibility**: existing tests/behavior do not regress when `SetPlayTime` is
  never called.

## File List (implementation)

- `karaoke.go` / `karaoke_test.go`: internal `word` type + time-driven pure logic
  (line location, sung word count, next boundary).
- `lyricline.go` / `lyricline_test.go`: plain lines use a RichText; karaoke lines
  use single-shaping + clipped overlay.
- `lyricsviewer.go` / `lyricsviewer_test.go`: `SetLyricLines`/`SetLyrics`,
  `LyricLine`/`LyricSegment`, `SetPlayTime`, predictive timer, line switching.
- `line_transition_test.go`: line-transition word-progress regression.
- `demo/karaoke/main.go`: demo; parses line and inline tags into the
  `LyricLine` model itself.
