package appconfig

import (
	"strings"
	"testing"
)

func TestDefaultLifecycleSafetyStopsMotionAndRefreshes(t *testing.T) {
	value := Defaults().Integrations.Lifecycle
	if value.SessionLock != LifecycleActionStopMotion ||
		value.Suspend != LifecycleActionStopMotion || !value.RefreshOnResume {
		t.Fatalf("default lifecycle safety=%+v", value)
	}
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
}

func TestLifecycleSafetyValidationRejectsUnknownActions(t *testing.T) {
	for _, field := range []string{"session", "suspend"} {
		t.Run(field, func(t *testing.T) {
			value := Defaults()
			if field == "session" {
				value.Integrations.Lifecycle.SessionLock = "shutdown"
			} else {
				value.Integrations.Lifecycle.Suspend = "shutdown"
			}
			err := value.Validate()
			if err == nil || !strings.Contains(err.Error(), "lifecycle_safety") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
