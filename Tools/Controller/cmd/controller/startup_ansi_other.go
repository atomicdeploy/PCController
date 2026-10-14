//go:build !windows

package main

import "io"

func enableStartupANSI(io.Writer) {}
