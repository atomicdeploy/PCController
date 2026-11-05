package main

import (
	"flag"
	"fmt"
	"os"

	"pccontroller.local/controller/internal/envfile"
	"pccontroller.local/controller/internal/tui"
)

func main() {
	if _, err := envfile.LoadProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "environment:", err)
		os.Exit(1)
	}
	pageName := flag.String("page", "dashboard", "dashboard, outputs, menus, board, app, rf, program, automate, events, or console")
	width := flag.Int("width", 132, "render width")
	height := flag.Int("height", 38, "render height")
	frame := flag.Int("frame", 0, "animation frame for welcome preview")
	all := flag.Bool("all", false, "render every page separated by a form feed")
	flag.Parse()
	if *pageName == "welcome" {
		fmt.Print(tui.WelcomeFrame("PCController", "Welcome · light and sound", *width, max(0, *frame), "Esc  Skip welcome"))
		return
	}
	if *pageName == "startup" {
		fmt.Print(tui.StartupFrame("PCController", "Starting terminal interface…", *width))
		return
	}
	if *all {
		for page := tui.PageDashboard; page <= tui.PageConsole; page++ {
			if page != tui.PageDashboard {
				fmt.Print("\n\f\n")
			}
			fmt.Print(tui.PreviewFrame(page, *width, *height))
		}
		return
	}
	page, err := tui.ParsePage(*pageName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Print(tui.PreviewFrame(page, *width, *height))
}
