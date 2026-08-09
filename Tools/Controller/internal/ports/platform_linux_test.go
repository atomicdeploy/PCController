//go:build linux

package ports

import (
	"testing"

	"github.com/fsnotify/fsnotify"
)

func TestLinuxSerialDeviceEventFiltersUnrelatedDevChanges(t *testing.T) {
	for _, name := range []string{"/dev/ttyUSB0", "/dev/ttyACM1", "/dev/ttyS0", "/dev/ttyAMA2", "/dev/rfcomm0"} {
		if !linuxSerialDeviceEvent(fsnotify.Event{Name: name, Op: fsnotify.Create}) {
			t.Fatalf("serial create %q was ignored", name)
		}
	}
	for _, event := range []fsnotify.Event{
		{Name: "/dev/nvme0", Op: fsnotify.Create},
		{Name: "/dev/ttyUSB0", Op: fsnotify.Write},
	} {
		if linuxSerialDeviceEvent(event) {
			t.Fatalf("unrelated event %+v was accepted", event)
		}
	}
}

func TestParseLinuxDeviceSelectorIsExact(t *testing.T) {
	filter, err := ParseSelector("/dev/serial/by-id/usb-controller")
	if err != nil || filter.Port != "/dev/serial/by-id/usb-controller" || filter.Name != "" {
		t.Fatalf("filter=%+v err=%v", filter, err)
	}
}
