package main

import "testing"

func TestRunRejectsIncompleteArguments(t *testing.T) {
	if exitCode := run(nil); exitCode != 1 {
		t.Fatalf("run(nil) = %d, want 1", exitCode)
	}
	if exitCode := run([]string{"unexpected"}); exitCode != 2 {
		t.Fatalf("run(unexpected) = %d, want 2", exitCode)
	}
}
