package control

import "testing"

func TestParseDisplayCommandUsesNamedTimingFlags(t *testing.T) {
	request, err := parseDisplayCommand([]string{"lcd", "2000", "Door", "open"})
	if err != nil || request.Text != "2000 Door open" || request.SpeedMS != 0 || request.DurationMS != 0 {
		t.Fatalf("numeric text was treated as a positional timing alias: %#v err=%v", request, err)
	}
	request, err = parseDisplayCommand([]string{"lcd", "--speed", "220ms", "--duration", "5s", "--", "2000", "Door", "open"})
	if err != nil || request.Text != "2000 Door open" || request.SpeedMS != 220 || request.DurationMS != 5000 {
		t.Fatalf("named display timing was not applied: %#v err=%v", request, err)
	}
	if _, err := parseDisplayCommand([]string{"lcd", "--hold", "2000", "Door open"}); err == nil {
		t.Fatal("retired display timing alias was accepted")
	}
}

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
