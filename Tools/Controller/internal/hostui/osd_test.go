package hostui

import (
	"pccontroller.local/desktop/osd"
	"strings"
	"testing"
	"time"
)

func TestOSDValidationAndActionAdmission(t *testing.T) {
	base := OSDRequest{Text: "Ready", Actions: []OSDAction{{Label: "Events", URI: "pccontroller://page/events"}}}
	r, err := ValidateOSD(base)
	if err != nil || r.DurationMS != 5000 || r.Position != "bottom-center" {
		t.Fatal(r, err)
	}
	for _, kind := range []string{"duration", "icon", "action", "empty", "size", "controls", "theme", "position", "actions"} {
		t.Run(kind, func(t *testing.T) {
			r := base
			r.Actions = append([]OSDAction(nil), base.Actions...)
			switch kind {
			case "duration":
				r.DurationMS = -1
			case "icon":
				r.Icon = "C:/any.png"
			case "action":
				r.Actions[0].URI = "powershell:unsafe"
			case "empty":
				r.Text = " "
			case "size":
				r.Text = strings.Repeat("x", 2049)
			case "controls":
				r.Text = "x\x00"
			case "theme":
				r.Theme = "blue"
			case "position":
				r.Position = "any"
			case "actions":
				r.Actions = make([]OSDAction, 6)
			}
			if _, err := ValidateOSD(r); err == nil {
				t.Fatal("accepted invalid OSD")
			}
		})
	}
	if base.DurationMS != 0 {
		t.Fatal("normalization changed caller request")
	}
}

func TestOSDTimelineAndMarqueeAreTimeBased(t *testing.T) {
	if a, _ := osd.Timeline(80*time.Millisecond, time.Second, false); a <= 0 || a >= 1 {
		t.Fatal(a)
	}
	if a, _ := osd.Timeline(500*time.Millisecond, time.Second, false); a != 1 {
		t.Fatal(a)
	}
	if a, _ := osd.Timeline(time.Second, time.Second, false); a != 0 {
		t.Fatal(a)
	}
	if a, s := osd.Timeline(10*time.Millisecond, time.Second, true); a != 1 || s != 0 {
		t.Fatal(a, s)
	}
	if x := osd.Marquee(time.Second, 200, 1, false); x != 0 {
		t.Fatal("missing initial pause", x)
	}
	if x := osd.Marquee(2100*time.Millisecond, 200, 1, false); x < 37 || x > 39 {
		t.Fatal(x)
	}
	if x := osd.Marquee(10*time.Second, 0, 1, false); x != 0 {
		t.Fatal(x)
	}
	if x := osd.Marquee(10*time.Second, 200, 1, true); x != 0 {
		t.Fatal("reduced motion scrolled", x)
	}
}

func TestOSDNotificationRetainsSemanticIdentityAndActions(t *testing.T) {
	n := Notification{ID: "event-1", Title: "Door", Body: "Door opened", Severity: "warning", Icon: "door", Actions: []NotificationAction{{Label: "Events", URI: "pccontroller://page/events"}}}
	r := OSDForNotification(n, 8000)
	if r.DurationMS != 8000 || r.Icon != "door" || r.ID != "event-1" || len(r.Actions) != 1 || r.Actions[0].URI != n.Actions[0].URI {
		t.Fatal(r)
	}
}

func TestOSDMultilineAndScriptChoices(t *testing.T) {
	r, err := ValidateOSD(OSDRequest{Text: "One\r\nTwo", Actions: []OSDAction{{ID: "confirm", Label: "Confirm"}}})
	if err != nil || r.Text != "One\nTwo" || r.Actions[0].URI != "" {
		t.Fatal(r, err)
	}
	for _, request := range []OSDRequest{
		{Text: "x", Behavior: "unlimited"},
		{Text: "x", Background: "file:///x"},
		{Text: strings.Repeat("line\n", 6) + "seventh"},
		{Text: "x", Actions: []OSDAction{{Label: "No identity"}}},
		{Text: "x", Actions: []OSDAction{{ID: "same", Label: "One"}, {ID: "same", Label: "Two"}}},
	} {
		if _, err := ValidateOSD(request); err == nil {
			t.Fatal("invalid multiline/choice request accepted", request)
		}
	}
}
