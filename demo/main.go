package main

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	fynesyncedlyrics "github.com/dweymouth/fyne-synced-lyrics"
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

	l := fynesyncedlyrics.NewSyncedLyricsViewer()
	l.SetLyrics(lyrics)

	win.SetContent(l)
	win.Resize(fyne.NewSize(200, 300))

	tick := time.NewTicker(1 * time.Second)
	counter := 0
	go func() {
		for {
			<-tick.C
			if counter == 16 {
				l.SetCurrentLine(3)
			} else if counter == 24 {
				l.SetLyrics(lyrics)
			} else {
				l.NextLine()
			}
			counter++
		}
	}()

	win.ShowAndRun()
}
