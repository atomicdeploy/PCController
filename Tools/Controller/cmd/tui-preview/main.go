package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"pccontroller.local/controller/internal/envfile"
	"pccontroller.local/controller/internal/tui"
)

func main() {
	if _, err := envfile.LoadProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "environment:", err)
		os.Exit(1)
	}
	pageName := flag.String("page", "dashboard", "dashboard, controls, menus, board, app, rf, program, automate, events, or console")
	width := flag.Int("width", 132, "render width")
	height := flag.Int("height", 38, "render height")
	frame := flag.Int("frame", 0, "animation frame for welcome preview")
	all := flag.Bool("all", false, "render every page separated by a form feed")
	format := flag.String("format", "ansi", "ansi or svg")
	flag.Parse()
	if *format != "ansi" && *format != "svg" {
		fmt.Fprintln(os.Stderr, "format must be ansi or svg")
		os.Exit(2)
	}
	if *format == "svg" {
		// An SVG is a true-color target even when stdout is redirected to a file.
		lipgloss.SetColorProfile(termenv.TrueColor)
		lipgloss.SetHasDarkBackground(true)
	}
	printFrame := func(frame string) {
		if *format == "svg" {
			fmt.Print(tui.PreviewSVG(frame))
		} else {
			fmt.Print(frame)
		}
	}
	if *pageName == "welcome" {
		printFrame(tui.WelcomeFrame("PCController", "Welcome · light and sound", *width, max(0, *frame), "Esc  Skip welcome"))
		return
	}
	if *pageName == "startup" {
		printFrame(tui.StartupFrame("PCController", "Starting terminal interface…", *width))
		return
	}
	if *all {
		if *format == "svg" {
			fmt.Fprintln(os.Stderr, "--all requires ansi format")
			os.Exit(2)
		}
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
	printFrame(tui.PreviewFrame(page, *width, *height))
}
