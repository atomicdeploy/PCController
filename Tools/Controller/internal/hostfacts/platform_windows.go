//go:build windows

package hostfacts

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

const (
	wbemFlagReturnImmediately = 0x10
	wbemFlagForwardOnly       = 0x20
	comSFalse                 = 0x00000001
)

func platformHostFactsSource() string { return "wmi" }

func platformHostFactsClass(_ string, windowsClass string) string { return windowsClass }

// nativeBackend talks directly to the local Windows Management
// Instrumentation COM automation API. It performs no process launch and does
// not accept a namespace, class, columns, or WQL string from a caller.
type nativeBackend struct{}

func (nativeBackend) query(
	ctx context.Context,
	spec querySpec,
) ([]map[string]any, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oleError *ole.OleError
		if !errors.As(err, &oleError) ||
			(oleError.Code() != ole.S_OK && oleError.Code() != comSFalse) {
			return nil, false, fmt.Errorf("initialize WMI COM apartment: %w", err)
		}
	}
	defer ole.CoUninitialize()

	locatorUnknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return nil, false, fmt.Errorf("create WMI locator: %w", err)
	}
	if locatorUnknown == nil {
		return nil, false, errors.New("create WMI locator: COM returned nil")
	}
	defer locatorUnknown.Release()

	locator, err := locatorUnknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, false, fmt.Errorf("open WMI locator automation interface: %w", err)
	}
	if locator == nil {
		return nil, false, errors.New("open WMI locator automation interface: COM returned nil")
	}
	defer locator.Release()

	serviceRaw, err := oleutil.CallMethod(locator, "ConnectServer", ".", `root\CIMV2`)
	if err != nil {
		return nil, false, fmt.Errorf("connect local WMI namespace: %w", err)
	}
	if serviceRaw == nil || serviceRaw.ToIDispatch() == nil {
		if serviceRaw != nil {
			_ = serviceRaw.Clear()
		}
		return nil, false, errors.New("connect local WMI namespace: COM returned nil")
	}
	service := serviceRaw.ToIDispatch()
	defer serviceRaw.Clear()

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	resultRaw, err := oleutil.CallMethod(
		service,
		"ExecQuery",
		spec.wql,
		"WQL",
		int32(wbemFlagReturnImmediately|wbemFlagForwardOnly),
	)
	if err != nil {
		return nil, false, fmt.Errorf("execute WMI profile %s: %w", spec.Profile, err)
	}
	if resultRaw == nil || resultRaw.ToIDispatch() == nil {
		if resultRaw != nil {
			_ = resultRaw.Clear()
		}
		return nil, false, fmt.Errorf("execute WMI profile %s: COM returned nil", spec.Profile)
	}
	result := resultRaw.ToIDispatch()
	defer resultRaw.Clear()

	enumProperty, err := result.GetProperty("_NewEnum")
	if err != nil {
		return nil, false, fmt.Errorf("enumerate WMI profile %s: %w", spec.Profile, err)
	}
	if enumProperty == nil || enumProperty.ToIUnknown() == nil {
		if enumProperty != nil {
			_ = enumProperty.Clear()
		}
		return nil, false, fmt.Errorf("enumerate WMI profile %s: COM returned nil", spec.Profile)
	}
	defer enumProperty.Clear()

	enumerator, err := enumProperty.ToIUnknown().IEnumVARIANT(ole.IID_IEnumVariant)
	if err != nil {
		return nil, false, fmt.Errorf("open WMI profile %s enumerator: %w", spec.Profile, err)
	}
	if enumerator == nil {
		return nil, false, fmt.Errorf("open WMI profile %s enumerator: COM returned nil", spec.Profile)
	}
	defer enumerator.Release()

	limit := spec.MaxRows
	if limit <= 0 || limit > maxGlobalRows {
		limit = maxGlobalRows
	}
	rows := make([]map[string]any, 0, limit)
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		itemRaw, count, nextErr := enumerator.Next(1)
		if count == 0 {
			return rows, false, nil
		}
		if nextErr != nil {
			return nil, false, fmt.Errorf("read WMI profile %s row: %w", spec.Profile, nextErr)
		}
		if len(rows) >= limit {
			if item := itemRaw.ToIDispatch(); item != nil {
				item.Release()
			}
			return rows, true, nil
		}
		item := itemRaw.ToIDispatch()
		if item == nil {
			return nil, false, fmt.Errorf("read WMI profile %s row: COM returned nil", spec.Profile)
		}
		row, rowErr := readWMIColumns(item, spec.Columns)
		item.Release()
		if rowErr != nil {
			return nil, false, fmt.Errorf("read WMI profile %s row: %w", spec.Profile, rowErr)
		}
		rows = append(rows, row)
	}
}

func readWMIColumns(item *ole.IDispatch, columns []string) (map[string]any, error) {
	row := make(map[string]any, len(columns))
	for _, column := range columns {
		property, err := item.GetProperty(column)
		if err != nil {
			return nil, fmt.Errorf("property %s: %w", column, err)
		}
		if property == nil {
			continue
		}
		value := property.Value()
		if clearErr := property.Clear(); clearErr != nil {
			return nil, fmt.Errorf("release property %s: %w", column, clearErr)
		}
		row[column] = value
	}
	return row, nil
}
