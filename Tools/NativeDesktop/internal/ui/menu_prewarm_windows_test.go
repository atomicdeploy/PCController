//go:build windows

package ui

import (
	"reflect"
	"testing"
)

func TestAppMenuPrewarmTargetSizesPrioritizeCurrentMonitor(t *testing.T) {
	got := appMenuPrewarmTargetSizes(110)
	want := []int32{25, 22, 28, 33, 39, 44, 50, 55, 66, 88}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prewarm target sizes = %v, want %v", got, want)
	}
}

func TestAppMenuPrewarmTargetSizesDeduplicateStandardCurrentDPI(t *testing.T) {
	got := appMenuPrewarmTargetSizes(144)
	want := []int32{33, 22, 28, 39, 44, 50, 55, 66, 88}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prewarm target sizes = %v, want %v", got, want)
	}
}

func TestAppMenuPrewarmTargetSizesRejectInvalidCurrentDPI(t *testing.T) {
	got := appMenuPrewarmTargetSizes(0)
	want := []int32{22, 28, 33, 39, 44, 50, 55, 66, 88}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prewarm target sizes = %v, want %v", got, want)
	}
}
