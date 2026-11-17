// Command Sub2Surge is the macOS desktop app: it serves the embedded page and
// binds the Conversion service the page calls.
package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/egoist/mygo"
)

//go:embed all:web
var webFiles embed.FS

func main() {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}

	mygo.SetFrontend(web)
	mygo.App.SetName("Sub2Surge")

	converter := NewConverter()
	// The saved appearance has to be in place before the window exists,
	// otherwise the first frame paints with the system's and then switches.
	mygo.Theme.SetSource(mygo.ThemeSource(converter.Theme()))
	mygo.Bind(converter)

	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:           "Sub2Surge",
			URL:             "/",
			Width:           1040,
			Height:          800,
			MinWidth:        880,
			MinHeight:       680,
			StateKey:        "main",
			BackgroundColor: "light-dark(#f3f4f0, #121417)",
		})
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
