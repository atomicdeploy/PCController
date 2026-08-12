package control

import (
	"testing"
	"time"
)

func TestCheckedDisplayUint16RejectsOutOfRangeValues(t *testing.T) {
	for _, test := range []struct {
		value   int
		want    uint16
		wantErr bool
	}{
		{value: -1, wantErr: true},
		{value: 0, want: 0},
		{value: 65535, want: 65535},
		{value: 65536, wantErr: true},
	} {
		value, err := checkedDisplayUint16(test.value, "test")
		if (err != nil) != test.wantErr || value != test.want {
			t.Fatalf("checkedDisplayUint16(%d)=(%d, %v), want (%d, error=%t)",
				test.value, value, err, test.want, test.wantErr)
		}
	}
}

func TestLegacySegmentPlanUsesMCUTimingAndHostRepeatBoundaries(t *testing.T) {
	tests := []struct {
		name string
		in   DisplayRequest
		want legacySegmentPlan
	}{
		{
			name: "static once",
			in: DisplayRequest{Text: "REC1", SpeedMS: 220, DurationMS: 1000,
				Repeat: DisplayRepeatOnce},
			want: legacySegmentPlan{text: "REC1", durationMS: 1000},
		},
		{
			name: "static loop",
			in: DisplayRequest{Text: "LIVE", SpeedMS: 220, DurationMS: 1000,
				Repeat: DisplayRepeatLoop},
			want: legacySegmentPlan{text: "LIVE"},
		},
		{
			name: "one scroll",
			in: DisplayRequest{Text: "RECORD", SpeedMS: 200, DurationMS: 1000,
				Repeat: DisplayRepeatOnce},
			want: legacySegmentPlan{text: "RECORD", durationMS: 200,
				clearAfter: 1400 * time.Millisecond},
		},
		{
			name: "forced short interval",
			in: DisplayRequest{Text: "REC", Scroll: true, SpeedMS: 250,
				DurationMS: 1000, Repeat: DisplayRepeatInterval, IntervalMS: 3000},
			want: legacySegmentPlan{text: "REC  ", durationMS: 250,
				clearAfter: 1500 * time.Millisecond, repeatWait: 3000 * time.Millisecond},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := makeLegacySegmentPlan(test.in); got != test.want {
				t.Fatalf("legacy segment plan=%+v, want %+v", got, test.want)
			}
		})
	}
}
