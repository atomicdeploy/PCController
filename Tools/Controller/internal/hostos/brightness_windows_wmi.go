//go:build windows

package hostos

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	wmiReturnImmediately = 0x10
	wmiForwardOnly       = 0x20
	wmiCOMSFalse         = 0x00000001
)

// wmiMonitorBrightness reads the active integrated display through root\WMI.
func wmiMonitorBrightness(ctx context.Context) (BrightnessStatus, error) {
	var status BrightnessStatus
	err := withWMIService(ctx, func(service *ole.IDispatch) error {
		rows, err := wmiQuery(service,
			"SELECT Active, CurrentBrightness, InstanceName FROM WmiMonitorBrightness WHERE Active = TRUE")
		if err != nil {
			return err
		}
		defer rows.Release()
		found := false
		err = oleutil.ForEach(rows, func(value *ole.VARIANT) error {
			defer value.Clear()
			if found {
				return nil
			}
			item := value.ToIDispatch()
			if item == nil {
				return errors.New("WmiMonitorBrightness returned a non-object row")
			}
			current, err := wmiUint8Property(item, "CurrentBrightness")
			if err != nil {
				return err
			}
			name, _ := wmiStringProperty(item, "InstanceName")
			name = strings.TrimSpace(name)
			if name == "" {
				name = "integrated display"
			}
			status = BrightnessStatus{
				Percent: int(current), RawCurrent: uint32(current), RawMaximum: 100,
				Display: name, Backend: "wmi-laptop", Integrated: true,
			}
			found = true
			return nil
		})
		if err != nil {
			return err
		}
		if !found {
			return errors.New("no active WmiMonitorBrightness laptop panel was found")
		}
		return nil
	})
	return status, err
}

// wmiSetMonitorBrightness writes the active laptop panel and reads it back.
func wmiSetMonitorBrightness(ctx context.Context, percent int) (BrightnessStatus, error) {
	if percent < 0 || percent > 100 {
		return BrightnessStatus{}, fmt.Errorf("monitor brightness %d is outside 0..100", percent)
	}
	changed := false
	err := withWMIService(ctx, func(service *ole.IDispatch) error {
		rows, err := wmiQuery(service,
			"SELECT Active, InstanceName FROM WmiMonitorBrightnessMethods WHERE Active = TRUE")
		if err != nil {
			return err
		}
		defer rows.Release()
		err = oleutil.ForEach(rows, func(value *ole.VARIANT) error {
			defer value.Clear()
			if changed {
				return nil
			}
			item := value.ToIDispatch()
			if item == nil {
				return errors.New("WmiMonitorBrightnessMethods returned a non-object row")
			}
			result, callErr := oleutil.CallMethod(item, "WmiSetBrightness", uint32(0), uint8(percent))
			if result != nil {
				defer result.Clear()
			}
			if callErr != nil {
				return fmt.Errorf("WmiSetBrightness(%d): %w", percent, callErr)
			}
			changed = true
			return nil
		})
		if err != nil {
			return err
		}
		if !changed {
			return errors.New("no active WmiMonitorBrightnessMethods laptop panel was found")
		}
		return nil
	})
	if err != nil {
		return BrightnessStatus{}, err
	}
	status, readErr := wmiMonitorBrightness(ctx)
	if readErr == nil {
		return status, nil
	}
	return BrightnessStatus{
		Percent: percent, RawCurrent: uint32(percent), RawMaximum: 100,
		Display: "integrated display", Backend: "wmi-laptop", Integrated: true,
	}, nil
}

// withWMIService owns the COM apartment and exposes only the fixed local WMI
// namespace used for integrated-panel brightness; no shell process is used.
func withWMIService(ctx context.Context, action func(*ole.IDispatch) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oleErr *ole.OleError
		if !errors.As(err, &oleErr) ||
			(oleErr.Code() != ole.S_OK && oleErr.Code() != wmiCOMSFalse) {
			return fmt.Errorf("initialize laptop-panel WMI: %w", err)
		}
	}
	defer ole.CoUninitialize()

	locatorUnknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return fmt.Errorf("create laptop-panel WMI locator: %w", err)
	}
	if locatorUnknown == nil {
		return errors.New("create laptop-panel WMI locator: COM returned nil")
	}
	defer locatorUnknown.Release()
	locator, err := locatorUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("open laptop-panel WMI locator: %w", err)
	}
	if locator == nil {
		return errors.New("open laptop-panel WMI locator: COM returned nil")
	}
	defer locator.Release()
	serviceResult, err := oleutil.CallMethod(locator, "ConnectServer", ".", `root\WMI`)
	if err != nil {
		return fmt.Errorf("connect laptop-panel WMI namespace: %w", err)
	}
	if serviceResult == nil || serviceResult.ToIDispatch() == nil {
		if serviceResult != nil {
			_ = serviceResult.Clear()
		}
		return errors.New("connect laptop-panel WMI namespace: COM returned nil")
	}
	defer serviceResult.Clear()
	if err := ctx.Err(); err != nil {
		return err
	}
	return action(serviceResult.ToIDispatch())
}

func wmiQuery(service *ole.IDispatch, query string) (*ole.IDispatch, error) {
	result, err := oleutil.CallMethod(service, "ExecQuery", query, "WQL",
		int32(wmiReturnImmediately|wmiForwardOnly))
	if err != nil {
		return nil, fmt.Errorf("query laptop-panel WMI: %w", err)
	}
	if result == nil || result.ToIDispatch() == nil {
		if result != nil {
			_ = result.Clear()
		}
		return nil, errors.New("query laptop-panel WMI: COM returned nil")
	}
	dispatch := result.ToIDispatch()
	// Keep one reference after clearing the result VARIANT owned by this call.
	dispatch.AddRef()
	_ = result.Clear()
	return dispatch, nil
}

func wmiUint8Property(item *ole.IDispatch, name string) (uint8, error) {
	property, err := oleutil.GetProperty(item, name)
	if err != nil {
		return 0, fmt.Errorf("read laptop-panel WMI %s: %w", name, err)
	}
	if property == nil {
		return 0, fmt.Errorf("read laptop-panel WMI %s: COM returned nil", name)
	}
	defer property.Clear()
	switch value := property.Value().(type) {
	case uint8:
		return value, nil
	case int8:
		if value >= 0 {
			return uint8(value), nil
		}
	case int16:
		if value >= 0 && value <= 100 {
			return uint8(value), nil
		}
	case int32:
		if value >= 0 && value <= 100 {
			return uint8(value), nil
		}
	case uint16:
		if value <= 100 {
			return uint8(value), nil
		}
	case uint32:
		if value <= 100 {
			return uint8(value), nil
		}
	}
	return 0, fmt.Errorf("read laptop-panel WMI %s: invalid value %v", name, property.Value())
}

func wmiStringProperty(item *ole.IDispatch, name string) (string, error) {
	property, err := oleutil.GetProperty(item, name)
	if err != nil {
		return "", fmt.Errorf("read laptop-panel WMI %s: %w", name, err)
	}
	if property == nil {
		return "", fmt.Errorf("read laptop-panel WMI %s: COM returned nil", name)
	}
	defer property.Clear()
	value, ok := property.Value().(string)
	if !ok {
		return "", fmt.Errorf("read laptop-panel WMI %s: invalid value %v", name, property.Value())
	}
	return value, nil
}
