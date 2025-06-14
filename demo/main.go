package main

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fynelyrics "github.com/supersonic-app/fyne-lyrics"
)

var lyrics = []string{
	"Hello world",
	"These are my song lyrics",
	"Let's add some more",
	"And even some more",
	"And now yet another",
	"And how about even one more to see",
	"And just so we have a long enough song...",
	"Here we go to repeat.",
	"Hello world",
	"These are my song lyrics",
	"Let's add some more",
	"And even some more",
	"And now yet another",
	"And how about even one more to see",
	"And just so we have a long enough song...",
	"Here we go to repeat",
	"Just kidding we're done.",
}

func main() {
	app := app.New()
	win := app.NewWindow("Synced Lyrics Demo")

	l := fynelyrics.NewLyricsViewer()
	l.ActiveLyricPosition = fynelyrics.ActiveLyricPositionUpperMiddle
	l.SetLyrics(lyrics, true /*synced*/)
	l.OnLyricTapped = func(lineNum int) {
		log.Printf("Tapped lyric line %d", lineNum)
	}

	win.SetContent(l)
	win.Resize(fyne.NewSize(200, 300))

	tick := time.NewTicker(1 * time.Second)
	counter := 0
	go func() {
		for {
			<-tick.C
			if counter == 5 {
				// demonstrate centering lyrics
				l.Alignment = fyne.TextAlignCenter
				l.Refresh()
			} else if counter == 17 {
				// demonstrate changing current line
				l.SetCurrentLine(1)
			} else if counter == 24 {
				// demonstrate unsynced mode
				l.SetLyrics(lyrics, false)
				break
			} else {
				l.NextLine()
			}
			counter++
		}
	}()

	win.ShowAndRun()
}
