//go:build !linux

package main

import (
	"io"
	"strings"
	"testing"
)

func TestHostProvisionReportsUnsupportedPlatformWithoutMutation(t *testing.T) {
	err := runToolchainHostProvision([]string{"--target-user", "operator", "--apply"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "supported only on Linux") {
		t.Fatalf("unsupported host provision error=%v", err)
	}
}
