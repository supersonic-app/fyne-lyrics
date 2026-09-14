# 行内卡拉OK（逐字高亮）支持 — 设计文档

日期：2026-09-10
状态：已实现

## 背景与目标

fyne-lyrics 是一个 Fyne 歌词显示 widget，支持 synced / unsynced 两种模式，
原本由外部通过 `NextLine()` / `SetCurrentLine(n)` 以"行"为粒度驱动。

需要兼容带行内标签的逐字歌词格式，例如：

```lrc
[00:00.00]<00:00.00>床<00:00.40>前<00:00.80>明<00:01.20>月<00:01.60>光<00:02.00> <00:02.40>(Moonlight)<00:03.00>...
```

即每一行文本中内嵌 `<MM:SS.CC>` 标签，指示其后字符（字/词）的开始时间（`CC` 为百分之一秒/2 位，或毫秒/3 位）。
目标：在活动行内，随播放时间逐字点亮（卡拉OK效果）。

### 与下游（supersonic）的关系

**解析在本工程之外（supersonic）**：supersonic 的歌词层负责把各来源解析成规范模型
（行级 `[...]` + 行内逐字 `<...>` 时间戳），再交给 fyne-lyrics。fyne-lyrics **不解析**
原始文本，只接收带时间的数据，并做时间同步与渲染。

- lrclib 路径：从含 `<...>` 行内标签的 synced LRC 解析出行起始 + 逐字分段。
- subsonic 结构化路径：每行 `Text` + `Start`，无逐字分段（整行一个段）。

因此 widget 的输入是"规范歌词模型"，既可带逐字时间，也可只有行时间。

## 设计原则

- **完全向后兼容**：现有 `SetLyrics`/`SetCurrentLine`/`NextLine`/`OnLyricTapped` 行为不变。
- **解析与渲染分离**：时间戳解析归调用方；时间同步与渲染归本 widget。
- **模式由"是否调用 SetPlayTime"决定**：
  - 从未调用 → 保持现有 `NextLine`/`SetCurrentLine` 行为原样。
  - 开始调用 → 进入"时间驱动模式"，widget 随每次 `SetPlayTime` 自主推进行切换、字符点亮与滚动。
- **性能**：push 提供时间基线 + 短距预测定时器修正字边界精度；仅在状态变化时重绘。

## 新增公开 API

### 1. 播放时间输入（push 模式）

```go
// 外部在每个播放时间更新时调用。curTime 为当前播放时间（秒），
// seeked 表示本次更新是否源于 seek。
// 从未调用 SetPlayTime 时，widget 保持现有 NextLine / SetCurrentLine 行为；
// 一旦调用，进入时间驱动模式。
// 必须在 UI 线程调用：它会修改 widget 状态并启停动画。
func (l *LyricsViewer) SetPlayTime(curTime float64, seeked bool)
```

采用 push 而非 pull（函数句柄）：

- supersonic 已有 `OnPlayTimeUpdate` 轮询循环（普通 250ms、waveform 时 100ms），
  设置一次回调直接转发即可，外部零额外负担。
- 调用粒度由供给方轮询频率决定（250ms 量级），非"逐字高频"，无性能顾虑。
- widget 只维护一个**短距预测定时器**用于字边界精度修正，无长驻 goroutine、无自建调度循环。

### 2. 歌词输入（规范模型，调用方解析）

```go
type LyricSegment struct {
    Text  string
    Start float64 // 秒；<0 = 无时间
}
type LyricLine struct {
    Start    float64
    Segments []LyricSegment
}
// 传入已解析的规范模型。调用方负责解析原始格式（LRC 等）。
func (l *LyricsViewer) SetLyricLines(lines []LyricLine, synced bool)
```

### 3. 兼容：旧 SetLyrics + LineStarts

```go
// 普通歌词：每行视为单个无时间片段，行起始由 LineStarts 提供（可选）
func (l *LyricsViewer) SetLyrics(lines []string, synced bool)
LineStarts []float64 // 仅旧 SetLyrics 路径使用
```

## 输入模型与职责边界

- **解析（行级 + 行内）**：调用方（supersonic）。时间格式的解析只有这一处，精度一致。
- **时间同步 + 渲染**：本 widget。收到 `LyricLine{Start, Segments}` 与当前播放时间后，
  自行定位活动行/活动字、滚动、高亮，并用预测定时器在两次 push 之间补字边界。
- 内部把 `LyricSegment` 映射为内部 `word{text, start}`；`start < 0` 表示无时间。
- 未提供 `Segments` 的行（普通歌词）视为单个无时间片段，走回退显示。

## 渲染（时间驱动模式）

### 关键约束：字体回退与基线

在真实 GL 驱动（尤其 macOS + supersonic 的 fyne fork）下，**每个独立文本对象会各自做
字体回退（font fallback）**。把一行拆成多个文本对象（每字/每词一个）时，同一个字形
可能解析到不同的回退字体，**基线不同 → 字与字之间高低错位**。

注意：`RenderedTextSize` 会报告各字 baseline 相同（如全部 19.23），但实际绘制不同——
**测量值不可靠**。因此"按测量基线手动对齐多个文本对象"无法解决该问题。

**唯一可靠的方案是一次 shaping**：go-text 在排版**单个字符串**时，会把所有回退 run
对齐到同一基线；拆成多个字符串则各管各的。

### 实现：整行 base + 同一字符串裁剪叠加

`lyricLine` 不再为每个词建独立文本段，而是：

- `baseText`：整行文本（所有词拼接）的一个 `canvas.Text`，`InactiveLyricColor` 色
- `activeText`：**与 baseText 完全相同的整行字符串**的另一个 `canvas.Text`，`ActiveLyricColor` 色
- `activeText` 外套一个裁剪控件 `lyricClip`，裁剪宽度 = 已唱前缀的**像素宽度**

原理：

- 两层字符串完全相同 → shaping 逐字一致 → 字形像素级重合，**无重影**
- 裁剪只露出已唱前缀 → 实现逐字高亮
- 裁剪宽度用 `canvas.Text` 测量前缀（与渲染同一条测量路径），避免边界切进下一个字

**裁剪的跨版本兼容**（关键）：

| Fyne 版本 | 裁剪触发机制 |
|---|---|
| Fyne 2.5.x | 对象实现 `fyne.Scrollable` |
| supersonic 的 fyne fork | renderer 实现 `IsClip()` |

`lyricClip` **同时实现两者**，故两个版本都能正确裁剪。
synced 模式下 `scroll.Direction = ScrollNone`（本就不能滚动），因此用 `Scrollable`
标记不会影响滚动；unsynced 模式无行内标签、不产生多词行，也就不会用到裁剪。

### 行窗口推导

行的活动时间窗口为 `[本行 Start, 下一行 Start)`（`Start` 来自 `LyricLine.Start`）。
无时间的行使用 InactiveLyricColor 静态显示。

### 活动行滚动

沿用现有 `offsetForLine` 逻辑：

- 普通行切换：动画滚动到精确目标偏移（`offsetForLine(currentLine)`）并居中
- `seeked=true`（seek）：**瞬时定位**到精确偏移、不带动画
- 逐字点亮只更新叠加层的裁剪宽度，不触发整行滚动

## 内部推进：push + 预测定时器

时间驱动模式下，`SetPlayTime`（约 250ms 一次）提供粗粒度时间基线，内部用一个
**短距预测定时器**补足字边界精度，让字在其 `start` 时刻精确点亮，而不是等到下一次 push。

预测定时器以 **`fyne.Animation`** 实现（而非 `time.AfterFunc`）：Fyne 2.5.3 无 `fyne.Do`，
而 `fyne.Animation` 的回调在 UI 线程运行、widget 不可见时自动停止，无需自建
goroutine 或线程调度。每次 `SetPlayTime` 会取消并重排该动画。

**每次 `SetPlayTime(curTime, seeked)`：**

1. 记录 `lastCurTime = curTime`、`lastPushAt = now`（墙上时钟）；取消已挂起的预测动画。
2. 以 `curTime` 定位当前行 + `activeWordIdx`（`seeked=true` 强制全量；否则增量）。
3. 找下一个字/行边界 `T_next`（> `curTime` 的最近一个）。
4. 若 `T_next - curTime <= predictiveHorizon`（1s），挂起 `fyne.Animation(T_next - curTime)`；
   否则不挂（等下次 push）。

**预测动画完成时（回调在 UI 线程）：**

1. `elapsed = now - lastPushAt`。
2. **Stall 检测**：若 `elapsed > stallThreshold`（600ms，约 2.5× 250ms poll）→
   判定暂停/停止，取消后续调度并返回（下次 `SetPlayTime` 恢复）。
3. 否则：点亮当前行内 `start <= T_next` 的字（推进 `activeWordIdx`），必要时切换行/滚动。
4. 找下一个边界，按步骤 4 规则继续挂起。

**暂停安全性**：supersonic 暂停时 `OnPlayTimeUpdate` 停推（已确认 `OnPaused → stopPollTimePos`）。
纯 push 天然冻结；预测定时器靠 stall 检测保持暂停安全——暂停期间最多误推进约 `stallThreshold`
（约 1 字），恢复后首推立即按真实 `curTime` 纠正。

**边界**：

- `predictiveHorizon` 限制单次预测跨度，避免间奏段挂远期定时器做长程墙钟假设。
- 倍速播放（rate≠1）下预测有偏差，由下次 push 纠正，可接受。
- `SetLyrics` / widget 销毁 / `seeked=true` 时取消挂起的预测动画。

### 渲染开销

每次定位变化时重建整行 base/active 两个 `canvas.Text` 的文本与颜色。
每行只有 2 个文本对象，代价可忽略；不做逐段增量着色。

## 高亮粒度（v1 范围）

- **逐字变色**（默认、v1 实现）：字到达其 `start` 时，裁剪宽度推进到该字之后，
  对应字切为 Active 色。
- **平滑渐变**（可选增强，暂不实现）：字与字之间颜色随进度插值渐变，
  需要每帧重绘，成本高。YAGNI，留作后续。

## 兼容性与集成

- 旧 API（`SetLyrics`/`SetCurrentLine`/`NextLine`/`OnLyricTapped`）全部保留；
  `SetLyrics` 把每行作为单个无时间片段。
- 从未调用 `SetPlayTime` 时，行为与现在完全一致。
- 普通（无逐字分段）歌词在时间驱动模式下照常显示，只是没有逐字效果。
- supersonic 集成：把歌词**解析**为 `[]fynelyrics.LyricLine` 后调用 `SetLyricLines`，
  并把播放位置转发给 `SetPlayTime`：

```go
pm.OnPlayTimeUpdate(func(cur, _ float64, seeked bool) {
    fyne.Do(func() { lyricsViewer.SetPlayTime(cur, seeked) })
})
```

其 `UpdatePlayPos`/`OnSeeked` 中驱动 `NextLine`/`SetCurrentLine` 的逻辑停用，
改为直接转发 `SetPlayTime`。

## 错误与边界处理

- 歌词行为空文本 / 空 `Segments` → 显示空行。
- 行/段时间为负或异常 → 该行/段视为无时间，静态显示。
- `LineStarts`（旧路径）长度与 lines 不一致 → 以行首/静态处理。
- `SetPlayTime` 的 `curTime` 为负值或明显异常 → 视为 0，按最前位置处理。
- `advanceTo` 在 `currentLine == 0`（尚未进入任何行）时安全跳过，不越界。
- 重复调用 `SetLyricLines`/`SetLyrics` → 全量重置（当前行回首行、清零 `activeWordIdx`、
  重置 `timeDriven`），并取消挂起的预测动画。
- 一次播放时间更新期间多次调用 `SetPlayTime` → 幂等，仅在定位变化时重绘。

## 测试策略

- **纯逻辑**（`karaoke_test.go`）：`locateActiveLine` 行定位、`countActiveWords` 已唱字数、`nextBoundary` 下一边界。
- **视图**（`lyricsviewer_test.go`）：`SetLyricLines` 使用 `Segments`/`Start`；`SetLyrics` 把普通行包成单段并使用 `LineStarts`。
- **渲染**（`lyricline_test.go`）：多词行的 base 与 active 层字符串完全一致、位置重合、存在裁剪叠加层；
  普通行走 RichText 且活动色正确。
- **行切换**（`line_transition_test.go`）：跨行边界时新行的 `activeWordIdx` 用目标行词语计算，不出现"整行先全亮"。
- **兼容**：从未调用 `SetPlayTime` 时现有单测/行为不回归。

## 文件清单（实现）

- `karaoke.go` / `karaoke_test.go`：内部 `word` 类型 + 时间驱动纯逻辑（行定位、已唱字数、下一边界）。
- `lyricline.go` / `lyricline_test.go`：普通行 RichText；卡拉OK行单次 shaping + 裁剪叠加。
- `lyricsviewer.go` / `lyricsviewer_test.go`：`SetLyricLines`/`SetLyrics`、`LyricLine`/`LyricSegment`、
  `SetPlayTime`、预测定时器、行切换。
- `line_transition_test.go`：行切换逐字进度回归。
- `demo/karaoke/main.go`：demo；**自行解析**行级与行内标签为 `LyricLine` 模型。
