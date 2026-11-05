//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "tui-smoke requires Windows 10 version 1809 or newer (ConPTY)")
	os.Exit(2)
}
