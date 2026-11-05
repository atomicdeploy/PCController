//go:build windows

package winfx

import "testing"

func TestMirrorHorizontalEdgesIsAnInvolution(t *testing.T) {
	t.Parallel()
	left, right := mirrorHorizontalEdges(10, 210, 28, 74)
	if left != 146 || right != 192 {
		t.Fatalf("mirrored edges = %d..%d", left, right)
	}
	left, right = mirrorHorizontalEdges(10, 210, left, right)
	if left != 28 || right != 74 {
		t.Fatalf("round-trip edges = %d..%d", left, right)
	}
}
