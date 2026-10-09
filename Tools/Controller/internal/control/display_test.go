package control

import "testing"

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
