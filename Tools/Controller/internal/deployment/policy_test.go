package deployment

import "testing"

func TestResolveDeploymentPriority(t *testing.T) {
	for _, test := range []struct{ name, config, environment, explicit, want string }{
		{"default", "", "", "", Production},
		{"config", Development, "", "", Development},
		{"environment", Production, Development, "", Development},
		{"explicit", Development, Development, Production, Production},
		{"invalid-selected", Production, "auto", "", ""},
		{"explicit-overrides-invalid-environment", Production, "auto", Development, Development},
		{"alpha-is-not-classification", "", "", "0.1.0-alpha", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(Environment, test.environment)
			got, err := Resolve(test.config, test.explicit)
			if got != test.want || (err != nil) != (test.want == "") {
				t.Fatalf("got=%q err=%v want=%q", got, err, test.want)
			}
		})
	}
}

func TestDeploymentBackupRequirement(t *testing.T) {
	if RequiresBackup(Development, false) {
		t.Fatal("development iteration still requires raw backup")
	}
	for _, value := range []string{"", Production, "invalid"} {
		if !RequiresBackup(value, false) {
			t.Fatalf("%q bypassed backup", value)
		}
	}
	if !RequiresBackup(Development, true) {
		t.Fatal("EEPROM reset bypassed backup")
	}
}
