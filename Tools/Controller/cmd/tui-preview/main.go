package main

import (
	"flag"
	"fmt"
	"os"

	"pccontroller.local/controller/internal/tui"
)

func main() {
	pageName := flag.String("page", "dashboard", "dashboard, outputs, menus, board, app, rf, program, automate, events, or console")
	width := flag.Int("width", 132, "render width")
	height := flag.Int("height", 38, "render height")
	all := flag.Bool("all", false, "render every page separated by a form feed")
	flag.Parse()
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
