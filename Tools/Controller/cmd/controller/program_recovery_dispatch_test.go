package main

import "testing"

func TestProgramRecoveryDispatch(t *testing.T) {
	for _, command := range []string{"recover", "abandon", "RECOVER", "ABANDON"} {
		if !programUsesSharedEngine([]string{command, "target"}) {
			t.Fatalf("%s must use the shared durable recovery engine", command)
		}
	}
	for _, args := range [][]string{nil, {"flash"}, {"--operation", "read-flash"}, {"compile"}} {
		if programUsesSharedEngine(args) {
			t.Fatalf("unexpected recovery dispatch for %v", args)
		}
	}
}
