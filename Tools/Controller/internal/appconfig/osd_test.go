package appconfig

import "testing"

func TestOSDNotificationPreferencesValidateWithoutChangingDefaults(t *testing.T) {
	c := Defaults()
	if c.Integrations.Notifications.Presentation != "" {
		t.Fatal("default notification behavior changed")
	}
	c.Integrations.Notifications.Presentation = "osd"
	c.Integrations.Notifications.OSDDurationMS = 7000
	c.Integrations.Notifications.OSDBehavior = "stack"
	c.Integrations.Notifications.OSDPosition = "bottom-left"
	c.Integrations.Notifications.OSDBackground = "acrylic"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, ms := range []int{-1, 1, 499, 120001} {
		c.Integrations.Notifications.OSDDurationMS = ms
		if err := c.Validate(); err == nil {
			t.Fatal(ms)
		}
	}
	c.Integrations.Notifications.OSDDurationMS = 0
	c.Integrations.Notifications.Presentation = "unsafe"
	if err := c.Validate(); err == nil {
		t.Fatal("unknown presentation accepted")
	}
}
