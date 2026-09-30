package hostui

import "testing"

func TestFormatByteProgressUsesHumanReadableBinaryUnits(t *testing.T) {
	if got := FormatByteProgress(6419456, 6419456); got != "6.12 MiB / 6.12 MiB" {
		t.Fatalf("progress=%q", got)
	}
	if got := FormatBytes(1024); got != "1.00 KiB" {
		t.Fatalf("kilobytes=%q", got)
	}
}
