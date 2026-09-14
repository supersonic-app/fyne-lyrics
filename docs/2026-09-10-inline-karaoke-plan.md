# 行内卡拉OK（逐字高亮）实现计划

> **注意（后记）：** 本计划是最初执行记录，其中"解析在库内（`parseWords`）""逐词多 TextSegment 渲染"等已被最终实现取代。
> 最终架构：**时间戳解析归调用方（supersonic），库通过 `SetLyricLines`/`LyricLine`/`LyricSegment` 接收规范模型**，渲染为"整行单次 shaping + 裁剪叠加"。
> 以 `2026-09-10-inline-karaoke-design.md`（及英文版）为准。
>
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**提交策略：** 各任务实现**不单独 commit**，所有任务完成、最终 review 通过后由控制器一次性提交（见文末"最终提交"）。实现子代理的修改停留在工作区；任务 review 用 `git diff` (uncommitted) 与提交基线对比。

**Goal:** 让 fyne-lyrics 兼容 `<MM:SS.CC>字` 行内标签格式，在时间驱动模式下逐字点亮，并用一个公版中英混排歌词片段做一个可运行 demo。

**Architecture:** `SetLyrics` 收到的每行文本由新增解析器拆成 `[]word{text,start}`；`lyricLine` 从单 TextSegment 扩展为多 TextSegment，按 `activeWordIdx` 着色。新增 push 方法 `SetPlayTime(curTime, seeked)`：定位当前行 + `activeWordIdx`、增量重绘、必要时滚动；内部用一个**短距 `fyne.Animation`** 作为预测定时器补足字边界精度（fyne.Animation 回调在 UI 线程运行，且 v2.5.3 无 `fyne.Do`，此方案无需版本升级；widget 不可见时自动停表）。未调用 `SetPlayTime` 时现有 `NextLine`/`SetCurrentLine` 行为完全不变。

**Tech Stack:** Go 1.19，Fyne v2.5.3（`widget.RichText`、`fyne.Animation`、`container.Scroll`），标准 `testing`。

## Global Constraints

- 模块路径 `github.com/supersonic-app/fyne-lyrics`，Go 1.19，fyne v2.5.3（**不得升级** fyne 版本；不得使用 `fyne.Do`，因其 v2.5.3 不存在）。
- 现有公开 API（`SetLyrics`/`SetCurrentLine`/`NextLine`/`OnLyricTapped`/各 Color/Size 字段）签名与行为保持向后兼容。
- 时间驱动模式仅在调用过 `SetPlayTime` 后生效；该模式下 `NextLine`/`SetCurrentLine` 为 no-op。
- `<MM:SS.CC>` 的 `CC` 为百分之一秒（`start = min*60 + ss + cc/100`）。
- 不得在代码中添加注释（除非用户要求）；但 `//go:embed` 指令和 godoc 风格的导出 API 注释保留（遵循现有文件风格）。
- 测试用 `go test ./...`；纯逻辑提取为不依赖 Fyne 的函数以便单测。

## File Structure

- 新增 `wordparse.go`：`parseWords` 等纯函数（无 Fyne 依赖）。
- 新增 `wordparse_test.go`：解析单测。
- 新增 `karaoke.go`：时间驱动纯逻辑（`locateActiveLine`、`countActiveWords`、`nextBoundary`）+ 单测 `karaoke_test.go`。
- 修改 `lyricline.go`：`lyricLine` 改为 word-based 多段渲染，新增 `activeWordIdx`/`InactiveColorName`。
- 修改 `lyricsviewer.go`：`SetLyrics` 解析存 `lineWords`、新增 `LineStarts`/`SetPlayTime`、预测动画调度、时间驱动分支。
- 新增 `demo/karaoke/main.go`（内联一个公版中英混排示例歌词）：模拟播放、点按 seek 的 demo。

---

### Task 1: 行内标签解析器

**Files:**
- Create: `wordparse.go`
- Test: `wordparse_test.go`

**Interfaces:**
- Produces: `type word struct { text string; start float64 }`（`start=-1` 表示无时间）；`func parseWords(line string) []word`。

- [ ] **Step 1: 写失败测试**

Create `wordparse_test.go`:

```go
package fynelyrics

import "testing"

func TestParseWords(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []word
	}{
		{
			name: "tagged",
			in:   "<00:00.00>狂<00:00.36>人<00:00.72>日",
			want: []word{{"狂", 0.0}, {"人", 0.36}, {"日", 0.72}},
		},
		{
			name: "no tags",
			in:   "普通歌词行",
			want: []word{{"普通歌词行", -1}},
		},
		{
			name: "leading untagged",
			in:   "abc <01:05.05>def",
			want: []word{{"abc ", -1}, {"def", 65.05}},
		},
		{
			name: "empty tag text skipped",
			in:   "<00:00.00><00:00.10>人",
			want: []word{{"人", 0.10}},
		},
		{
			name: "minute wrap",
			in:   "<01:02.03>字",
			want: []word{{"字", 62.03}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseWords(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("len: got %d want %d (%v)", len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("seg %d: got %+v want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./ -run TestParseWords`
Expected: FAIL — `parseWords` undefined.

- [ ] **Step 3: 写最小实现**

Create `wordparse.go`:

```go
package fynelyrics

import (
	"regexp"
	"strconv"
)

type word struct {
	text  string
	start float64
}

var wordTagRe = regexp.MustCompile(`<(\d{2}):(\d{2})\.(\d{2})>`)

func parseWords(line string) []word {
	matches := wordTagRe.FindAllStringSubmatchIndex(line, -1)
	if len(matches) == 0 {
		return []word{{text: line, start: -1}}
	}
	var words []word
	if matches[0][0] > 0 {
		words = append(words, word{text: line[:matches[0][0]], start: -1})
	}
	for i, m := range matches {
		mm, _ := strconv.Atoi(line[m[2]:m[3]])
		ss, _ := strconv.Atoi(line[m[4]:m[5]])
		cc, _ := strconv.Atoi(line[m[6]:m[7]])
		start := float64(mm)*60 + float64(ss) + float64(cc)/100
		textStart := m[1]
		textEnd := len(line)
		if i+1 < len(matches) {
			textEnd = matches[i+1][0]
		}
		text := line[textStart:textEnd]
		if text == "" {
			continue
		}
		words = append(words, word{text: text, start: start})
	}
	return words
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./ -run TestParseWords`
Expected: PASS.

---

### Task 2: 时间驱动纯逻辑

**Files:**
- Create: `karaoke.go`
- Test: `karaoke_test.go`

**Interfaces:**
- Consumes: `word` (Task 1).
- Produces:
  - `func locateActiveLine(starts []float64, cur float64) int` — 返回应激活的 0-indexed 行；`cur < starts[0]` 时返回 -1。
  - `func countActiveWords(words []word, cur float64) int` — 前缀已唱字数（`start<0` 或 `start<=cur` 视为已唱，遇首个未唱即停）。
  - `func nextBoundary(words []word, lineStarts []float64, lineIdx int, cur float64) (float64, bool)` — 返回当前行内下一个未唱字的 `start`；行尾则返回下一行起点；无则 `ok=false`。

- [ ] **Step 1: 写失败测试**

Create `karaoke_test.go`:

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./ -run 'TestLocateActiveLine|TestCountActiveWords|TestNextBoundary'`
Expected: FAIL — functions undefined.

- [ ] **Step 3: 写最小实现**

Create `karaoke.go`:

```go
package fynelyrics

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
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./ -run 'TestLocateActiveLine|TestCountActiveWords|TestNextBoundary'`
Expected: PASS.

---

### Task 3: lyricLine 多段渲染

**Files:**
- Modify: `lyricline.go`
- Test: 手动 / 现有 demo 不回归（`go build ./...`）。

**Interfaces:**
- Consumes: `word` (Task 1).
- Produces: `lyricLine` 新增字段 `words []word`、`activeWordIdx int`（-1 全暗、len-1 全亮）、`InactiveColorName fyne.ThemeColorName`；`updateRichText` 按 `activeWordIdx` 着色每段。`newLyricLine` 改为接收 `[]word`。

- [ ] **Step 1: 重写 lyricline.go 为 word-based**

Replace `lyricline.go` content (保留 MouseIn/Out、Cursor、Tapped 等交互不变；只改数据与渲染):

```go
package fynelyrics

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type lyricLine struct {
	widget.BaseWidget

	words             []word
	activeWordIdx     int
	SizeName          fyne.ThemeSizeName
	ColorName         fyne.ThemeColorName
	InactiveColorName fyne.ThemeColorName
	HoveredColorName  fyne.ThemeColorName
	Alignment         fyne.TextAlign
	Tappable          bool

	onTapped func()
	hovered  bool
	richtext *widget.RichText
}

func newLyricLine(words []word, onTapped func()) *lyricLine {
	l := &lyricLine{
		words:          words,
		activeWordIdx:  -1,
		SizeName:       theme.SizeNameSubHeadingText,
		ColorName:      theme.ColorNameForeground,
		Alignment:      fyne.TextAlignLeading,
		onTapped:       onTapped,
	}
	l.ExtendBaseWidget(l)
	return l
}

var _ desktop.Hoverable = (*lyricLine)(nil)

func (l *lyricLine) MouseIn(*desktop.MouseEvent) {
	if l.Tappable {
		l.hovered = true
		l.Refresh()
	}
}

func (l *lyricLine) MouseMoved(*desktop.MouseEvent) {}

func (l *lyricLine) MouseOut() {
	l.hovered = false
	l.Refresh()
}

var _ desktop.Cursorable = (*lyricLine)(nil)

func (l *lyricLine) Cursor() desktop.Cursor {
	if l.Tappable {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

var _ fyne.Tappable = (*lyricLine)(nil)

func (l *lyricLine) Tapped(*fyne.PointEvent) {
	if l.Tappable {
		l.onTapped()
	}
}

func (l *lyricLine) updateRichText() {
	if l.richtext == nil {
		l.richtext = widget.NewRichText()
		l.richtext.Wrapping = fyne.TextWrapWord
	}
	segs := make([]widget.RichTextSegment, 0, len(l.words))
	for i, w := range l.words {
		style := widget.RichTextStyleSubHeading
		style.Alignment = l.Alignment
		style.SizeName = l.SizeName
		if i <= l.activeWordIdx {
			style.ColorName = l.colorFor(true)
		} else {
			style.ColorName = l.colorFor(false)
		}
		segs = append(segs, &widget.TextSegment{Style: style, Text: w.text})
	}
	if len(segs) == 0 {
		style := widget.RichTextStyleSubHeading
		style.Alignment = l.Alignment
		style.SizeName = l.SizeName
		style.ColorName = l.colorFor(false)
		segs = append(segs, &widget.TextSegment{Style: style, Text: ""})
	}
	l.richtext.Segments = segs
}

func (l *lyricLine) colorFor(active bool) fyne.ThemeColorName {
	if l.hovered && l.HoveredColorName != "" {
		return l.HoveredColorName
	}
	if active {
		return l.ColorName
	}
	if l.InactiveColorName != "" {
		return l.InactiveColorName
	}
	return theme.ColorNameDisabled
}

func (l *lyricLine) Refresh() {
	l.updateRichText()
	l.richtext.Refresh()
}

func (l *lyricLine) CreateRenderer() fyne.WidgetRenderer {
	l.updateRichText()
	return widget.NewSimpleRenderer(l.richtext)
}
```

- [ ] **Step 2: 编译等待 Task 4 完成（当前预期失败）**

Run: `go build ./...`
Expected: 编译错误来自 `lyricsviewer.go` 引用 `newLyricLine(text, ...)` 或 `ll.Text`。这属预期，Task 4 会同步修改 viewer 使编译通过。

---

### Task 4: LyricsViewer 时间驱动 + SetPlayTime + 预测定时器

**Files:**
- Modify: `lyricsviewer.go`
- Test: `lyricsviewer_test.go`（纯逻辑路径：定位/着色决策，不跑 Fyne 渲染）。

**Interfaces:**
- Consumes: `parseWords`、`locateActiveLine`、`countActiveWords`、`nextBoundary`、word-based `lyricLine`。
- Produces:
  - `func (l *LyricsViewer) SetPlayTime(curTime float64, seeked bool)`
  - 新字段 `LineStarts []float64`。
  - `SetLyrics` 内部解析每行为 `lineWords [][]word`；`newLyricLine` 改为传 `[]word`；`setLineColor` 改为 `setLineActive(ll, active bool)`（置 `activeWordIdx`）。

- [ ] **Step 1: 写失败测试（决策逻辑）**

Create `lyricsviewer_test.go`:

```go
package fynelyrics

import "testing"

func TestSetLyricsParsesWordsAndLineStarts(t *testing.T) {
	l := NewLyricsViewer()
	lines := []string{
		"<00:00.00>狂<00:00.36>人",
		"<00:07.29>词：佚名",
	}
	l.SetLyrics(lines, true)
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

func TestSetLyricsUsesLineStartsWhenProvided(t *testing.T) {
	l := NewLyricsViewer()
	l.LineStarts = []float64{1, 2, 3}
	l.SetLyrics([]string{"a", "b", "c"}, true)
	if l.lineStarts[0] != 1 || l.lineStarts[2] != 3 {
		t.Errorf("expected provided lineStarts, got %v", l.lineStarts)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./ -run 'TestSetLyricsParsesWordsAndLineStarts|TestSetLyricsUsesLineStartsWhenProvided'`
Expected: FAIL — `lineWords`/`lineStarts` undefined, `SetLyrics` 还没解析。

- [ ] **Step 3: 修改 lyricsviewer.go**

在 `LyricsViewer` 结构体中，`lines []string` 下方新增：

```go
	lineWords  [][]word
	lineStarts []float64

	timeDriven bool
	activeWord int
	lastCurTime float64
	lastPushAt  time.Time
	predAnim    *fyne.Animation
```

新增字段（公开）放在 `OnLyricTapped` 之后：

```go
	// LineStarts optionally provides each line's start time (seconds),
	// matching len(lines). If nil, starts are derived from each line's
	// first tagged word.
	LineStarts []float64
```

替换 `SetLyrics`：

```go
func (l *LyricsViewer) SetLyrics(lines []string, synced bool) {
	l.lines = lines
	l.synced = synced
	l.currentLine = 0
	l.activeWord = -1
	l.stopPredictive()
	l.lineWords = make([][]word, len(lines))
	for i, ln := range lines {
		l.lineWords[i] = parseWords(ln)
	}
	l.lineStarts = l.computeLineStarts()
	if l.scroll != nil {
		if synced {
			l.scroll.Direction = container.ScrollNone
		} else {
			l.scroll.Direction = container.ScrollVerticalOnly
		}
	}
	l.updateContent()
}

func (l *LyricsViewer) computeLineStarts() []float64 {
	if len(l.LineStarts) == len(l.lines) {
		out := make([]float64, len(l.LineStarts))
		copy(out, l.LineStarts)
		return out
	}
	out := make([]float64, len(l.lineWords))
	for i, words := range l.lineWords {
		start := -1.0
		for _, w := range words {
			if w.start >= 0 {
				start = w.start
				break
			}
		}
		out[i] = start
	}
	return out
}
```

替换 `newLyricLine`（签名改为传 `[]word`）：

```go
func (l *LyricsViewer) newLyricLine(lineNum int, useActiveColor bool) *lyricLine {
	var words []word
	if lineNum >= 1 && lineNum <= len(l.lineWords) {
		words = l.lineWords[lineNum-1]
	}
	ll := newLyricLine(words, nil)
	l.setLineProperties(ll, lineNum, false)
	ll.HoveredColorName = l.hoveredLyricColor()
	if useActiveColor {
		ll.ColorName = l.activeLyricColor()
		ll.InactiveColorName = l.inactiveLyricColor()
		ll.activeWordIdx = len(words) - 1
	} else {
		ll.ColorName = l.inactiveLyricColor()
		ll.InactiveColorName = l.inactiveLyricColor()
		ll.activeWordIdx = -1
	}
	return ll
}
```

替换 `setLineTextAndProperties`→`setLineProperties`（不再设 Text）：

```go
func (l *LyricsViewer) setLineProperties(ll *lyricLine, lineNum int, refresh bool) {
	ll.SizeName = l.textSizeName()
	ll.Alignment = l.Alignment
	ll.Tappable = l.synced && l.OnLyricTapped != nil && !l.timeDriven
	ll.onTapped = func() {
		if l.OnLyricTapped != nil {
			l.OnLyricTapped(lineNum)
		}
	}
	if refresh {
		ll.Refresh()
	}
}
```

替换 `setLineColor`→`setLineActive`：

```go
func (l *LyricsViewer) setLineActive(ll *lyricLine, active bool, refresh bool) {
	if active {
		ll.ColorName = l.activeLyricColor()
		ll.InactiveColorName = l.inactiveLyricColor()
		ll.activeWordIdx = len(ll.words) - 1
	} else {
		ll.ColorName = l.inactiveLyricColor()
		ll.InactiveColorName = l.inactiveLyricColor()
		ll.activeWordIdx = -1
	}
	if refresh {
		ll.Refresh()
	}
}
```

更新 `updateContent` 中对 `setLineTextAndProperties`/`setLineColor`/`newLyricLine(text,...)` 的调用为新的 `setLineProperties`/`setLineActive`/`newLyricLine(lineNum, useActiveColor)`。具体：把 `l.setLineTextAndProperties(rt, line, lineNum, true)` 改为 `l.setLineProperties(rt, lineNum, true)`；`l.setLineColor(rt, ..., true)` 改为 `l.setLineActive(rt, ..., true)`；`l.newLyricLine(line, lineNum, useActiveColor)` 改为 `l.newLyricLine(lineNum, useActiveColor)`。`prototypeLyricLineSize` 改用 `l.newLyricLine(0, false).MinSize()`。

在 `SetCurrentLine`/`NextLine` 开头加时间驱动 no-op：

```go
	if l.timeDriven {
		return
	}
```

新增 `SetPlayTime` 与预测动画：

```go
func (l *LyricsViewer) SetPlayTime(curTime float64, seeked bool) {
	if l.vbox == nil || !l.synced {
		l.lastCurTime = curTime
		return
	}
	if !l.timeDriven {
		l.timeDriven = true
		if l.OnLyricTapped != nil {
			for i := 1; i < len(l.vbox.Objects)-1; i++ {
				if ll, ok := l.vbox.Objects[i].(*lyricLine); ok {
					ll.Tappable = false
					ll.Refresh()
				}
			}
		}
	}
	l.stopPredictive()
	l.lastCurTime = curTime
	l.lastPushAt = time.Now()

	target := locateActiveLine(l.lineStarts, curTime)
	if target < 0 {
		l.setActiveLine(0, 0, seeked)
	} else {
		words := l.lineWords[target]
		active := countActiveWords(words, curTime)
		l.setActiveLine(target+1, active-1, seeked)
	}
	l.schedulePredictive()
}

func (l *LyricsViewer) setActiveLine(oneIndexedLine, activeWordIdx int, forceScroll bool) {
	if oneIndexedLine == l.currentLine && !forceScroll {
		l.updateActiveWord(activeWordIdx)
		return
	}
	l.checkStopAnimation()
	if l.currentLine > 0 && l.currentLine <= len(l.lines) {
		l.setLineActive(l.vbox.Objects[l.currentLine].(*lyricLine), false, true)
	}
	l.currentLine = oneIndexedLine
	var prevLine, nextLine *lyricLine
	if l.currentLine > 1 {
		prevLine = l.vbox.Objects[l.currentLine-1].(*lyricLine)
	}
	if l.currentLine >= 1 && l.currentLine <= len(l.lines) {
		nextLine = l.vbox.Objects[l.currentLine].(*lyricLine)
		l.setLineActive(nextLine, true, false)
		l.updateActiveWord(activeWordIdx)
	}
	l.setupScrollAnimation(prevLine, nextLine)
	l.anim.Start()
}

func (l *LyricsViewer) updateActiveWord(activeWordIdx int) {
	if l.currentLine < 1 || l.currentLine > len(l.lines) {
		return
	}
	ll := l.vbox.Objects[l.currentLine].(*lyricLine)
	if ll.activeWordIdx == activeWordIdx {
		return
	}
	ll.activeWordIdx = activeWordIdx
	ll.Refresh()
	l.activeWord = activeWordIdx
}

const predictiveHorizon = 1 * time.Second
const stallThreshold = 600 * time.Millisecond

func (l *LyricsViewer) schedulePredictive() {
	if l.currentLine < 1 || l.currentLine > len(l.lines) {
		return
	}
	words := l.lineWords[l.currentLine-1]
	b, ok := nextBoundary(words, l.lineStarts, l.currentLine-1, l.lastCurTime)
	if !ok {
		return
	}
	delta := b - l.lastCurTime
	if delta <= 0 || delta > predictiveHorizon {
		return
	}
	cur := l.lastCurTime
	l.predAnim = fyne.NewAnimation(delta, func(f float32) {
		if f < 1 {
			return
		}
		if time.Since(l.lastPushAt) > stallThreshold {
			l.predAnim = nil
			return
		}
		next := cur + delta
		l.lastCurTime = next
		words := l.lineWords[l.currentLine-1]
		active := countActiveWords(words, next)
		target := locateActiveLine(l.lineStarts, next)
		if target >= 0 && target+1 != l.currentLine {
			l.setActiveLine(target+1, active-1, false)
		} else {
			l.updateActiveWord(active - 1)
		}
		l.schedulePredictive()
	})
	l.predAnim.Curve = fyne.AnimationLinear
	l.predAnim.Start()
}

func (l *LyricsViewer) stopPredictive() {
	if l.predAnim != nil {
		l.predAnim.Stop()
		l.predAnim = nil
	}
}
```

在 `updateContent` 末尾、`SetLyrics` 重置时已调用 `stopPredictive`。在 `CreateRenderer` 之外无需改。确保 `lyricsviewer.go` 仍 `import "time"`（已 import）。

- [ ] **Step 4: 跑测试确认通过 + 编译**

Run: `go test ./...` 然后 `go build ./...`
Expected: 全部 PASS 且编译通过。

---

### Task 5: 公版中英混排示例的 demo

**Files:**
- Create: `demo/karaoke/main.go`（歌词内联为 raw string，不引入外部 lyric 文件）

**Interfaces:**
- Consumes: `NewLyricsViewer`、`SetLyrics`、`LineStarts`、`SetPlayTime`、`OnLyricTapped`、`ActiveLyricPosition`。
- 歌词数据为一个公版中英混排示例（如李白《静夜思》+ 自撰英文），内联为 `var lyricData = \`...\``。切勿内联任何有版权的歌词。

- [ ] **Step 1: 写 demo**

Create `demo/karaoke/main.go`，把公版示例歌词作为 raw string 赋给 `lyricData`：

```go
package main

import (
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fynelyrics "github.com/supersonic-app/fyne-lyrics"
)

// lyricData 是公版示例：李白《静夜思》+ 自撰英文，中英混排
var lyricData = `...`

var lineRe = regexp.MustCompile(`^\[(\d{2}):(\d{2}\.\d{1,3})\](.*)$`)
var metaRe = regexp.MustCompile(`^\[(ti|ar|al|by):`)

func main() {
	a := app.New()
	w := a.NewWindow("Karaoke Demo - Quiet Night Thoughts")

	var texts []string
	var starts []float64
	for _, raw := range strings.Split(lyricData, "\n") {
		raw = strings.TrimRight(raw, "\r")
		if raw == "" || metaRe.MatchString(raw) {
			continue
		}
		m := lineRe.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		mm, _ := strconv.Atoi(m[1])
		sec, _ := strconv.ParseFloat(m[2], 64)
		starts = append(starts, float64(mm)*60+sec)
		texts = append(texts, m[3])
	}
	if len(texts) != 10 {
		log.Fatalf("expected 10 lyric lines from the sample, got %d", len(texts))
	}

	viewer := fynelyrics.NewLyricsViewer()
	viewer.ActiveLyricPosition = fynelyrics.ActiveLyricPositionUpperMiddle
	viewer.Alignment = fyne.TextAlignCenter
	viewer.LineStarts = starts
	viewer.SetLyrics(texts, true)

	songDur := 5.0
	if len(starts) > 0 {
		songDur = starts[len(starts)-1] + 5
	}
	start := time.Now()
	viewer.OnLyricTapped = func(lineNum int) {
		if lineNum >= 1 && lineNum <= len(starts) {
			start = time.Now().Add(-time.Duration(starts[lineNum-1] * float64(time.Second)))
			viewer.SetPlayTime(starts[lineNum-1], true)
		}
	}

	w.SetContent(viewer)
	w.Resize(fyne.NewSize(360, 480))

	const pushInterval = 250 * time.Millisecond
	var lastPushed float64
	anim := fyne.NewAnimation(time.Duration(songDur*float64(time.Second)), func(f float32) {
		cur := time.Since(start).Seconds()
		if cur > songDur {
			cur = songDur
		}
		if cur-lastPushed >= pushInterval.Seconds() || cur >= songDur {
			viewer.SetPlayTime(cur, false)
			lastPushed = cur
		}
	})
	anim.Curve = fyne.AnimationLinear
	anim.Start()

	w.ShowAndRun()
}
```

- [ ] **Step 2: 编译 + vet**

Run: `go build ./demo/karaoke/ && go vet ./demo/karaoke/`
Expected: 编译通过，无 vet 错误。

- [ ] **Step 3: 冒烟运行（可选，需 GUI）**

Run: `go run ./demo/karaoke`
Expected: 窗口显示歌词，逐字点亮、逐行滚动；点按一行跳到对应时间。如无显示环境则跳过本步，仅以编译为准。

---

## 最终提交

所有任务完成、最终 review 通过后，由控制器一次性提交（不在各任务内单独 commit）：

```bash
git add wordparse.go wordparse_test.go karaoke.go karaoke_test.go lyricline.go lyricsviewer.go lyricsviewer_test.go demo/karaoke/main.go
git commit -m "Add inline karaoke word-by-word highlighting and demo"
```

---

## Self-Review（已执行）

**Spec coverage：** 解析（Task 1）✓；定位/窗口/边界纯逻辑（Task 2）✓；多段渲染+增量着色（Task 3）✓；`SetPlayTime`+预测定时器+stall（Task 4）✓；`LineStarts`（Task 4）✓；旧 API 兼容（Task 4 no-op）✓；demo（Task 5）✓。

**Placeholder scan：** 无 TBD/TODO；每步含实际代码。

**Type consistency：** `word`、`parseWords`、`locateActiveLine`、`countActiveWords`、`nextBoundary`、`setLineActive`、`setLineProperties`、`newLyricLine(lineNum, useActiveColor)` 在各 Task 间一致。`activeWordIdx` 语义（-1/len-1/中间）跨 Task 3/4 一致。

**注意：** 设计文档中"经 fyne.Do 回 UI 线程"在本计划改为 `fyne.Animation`（v2.5.3 无 `fyne.Do`；fyne.Animation 回调在 UI 线程、不可见自动停止），实现等价于设计意图，后续若升级 Fyne 可不改逻辑。
