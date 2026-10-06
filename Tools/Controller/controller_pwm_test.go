package controller

import "testing"

func TestShouldPublishPWMValuesDeduplicatesOnlyIdenticalSnapshots(t *testing.T) {
	client := &Client{}
	initial := PWMValues{Available: true, SelectedChannel: 11}
	initial.Values[11] = 642

	if !client.shouldPublishPWMValues(initial) {
		t.Fatal("first authoritative snapshot must be published")
	}
	if client.shouldPublishPWMValues(initial) {
		t.Fatal("identical authoritative snapshot must not be republished")
	}

	changed := initial
	changed.Values[11]++
	if !client.shouldPublishPWMValues(changed) {
		t.Fatal("changed channel value must be published")
	}
	changed.SelectedChannel = 12
	if !client.shouldPublishPWMValues(changed) {
		t.Fatal("changed selected channel must be published")
	}
}
