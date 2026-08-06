//go:build windows

package hostui

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidXMLDocumentIO            = windows.GUID{Data1: 0x6CD0E74E, Data2: 0xEE65, Data3: 0x4489, Data4: [8]byte{0x9E, 0xBF, 0xCA, 0x43, 0xE8, 0x7B, 0xA6, 0x37}}
	iidToastManagerStatics      = windows.GUID{Data1: 0x50AC103F, Data2: 0xD235, Data3: 0x4598, Data4: [8]byte{0xBB, 0xEF, 0x98, 0xFE, 0x4D, 0x1A, 0x3A, 0xD4}}
	iidToastNotificationFactory = windows.GUID{Data1: 0x04124B20, Data2: 0x82C6, Data3: 0x4229, Data4: [8]byte{0xB1, 0x09, 0xFD, 0x9E, 0xD4, 0x66, 0x2B, 0x53}}
)

// deliverWindowsToast uses the Windows Runtime ABI directly. Interactive
// actions remain protocol URIs in the XML and are activated through the native
// per-user URI registration; no command shell participates in delivery.
func deliverWindowsToast(ctx context.Context, payload []byte, appID string) error {
	return runWinRTApartment(ctx, func() error {
		document, err := activateWinRTInstance("Windows.Data.Xml.Dom.XmlDocument")
		if err != nil {
			return fmt.Errorf("create toast XML document: %w", err)
		}
		defer releaseCOM(document)
		documentIO, err := queryCOM(document, iidXMLDocumentIO)
		if err != nil {
			return fmt.Errorf("open toast XML loader: %w", err)
		}
		xml, err := newWindowsHString(string(payload))
		if err != nil {
			releaseCOM(documentIO)
			return err
		}
		loadErr := callCOM(documentIO, 6, uintptr(xml)).error("load toast XML")
		xml.delete()
		releaseCOM(documentIO)
		if loadErr != nil {
			return loadErr
		}

		manager, err := winRTActivationFactory(
			"Windows.UI.Notifications.ToastNotificationManager", iidToastManagerStatics,
		)
		if err != nil {
			return fmt.Errorf("open toast manager: %w", err)
		}
		application, err := newWindowsHString(appID)
		if err != nil {
			releaseCOM(manager)
			return err
		}
		var notifier unsafe.Pointer
		createNotifierErr := callCOM(
			manager, 7, uintptr(application), uintptr(unsafe.Pointer(&notifier)),
		).error("create toast notifier")
		application.delete()
		releaseCOM(manager)
		if createNotifierErr != nil {
			return createNotifierErr
		}
		defer releaseCOM(notifier)

		factory, err := winRTActivationFactory(
			"Windows.UI.Notifications.ToastNotification", iidToastNotificationFactory,
		)
		if err != nil {
			return fmt.Errorf("open toast notification factory: %w", err)
		}
		var toast unsafe.Pointer
		createToastErr := callCOM(
			factory, 6, uintptr(document), uintptr(unsafe.Pointer(&toast)),
		).error("create toast notification")
		releaseCOM(factory)
		if createToastErr != nil {
			return createToastErr
		}
		defer releaseCOM(toast)

		if err := ctx.Err(); err != nil {
			return err
		}
		showErr := callCOM(notifier, 6, uintptr(toast)).error("show toast notification")
		runtime.KeepAlive(payload)
		return showErr
	})
}
