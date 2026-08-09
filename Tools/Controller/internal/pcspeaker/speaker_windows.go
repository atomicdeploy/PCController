//go:build windows

package pcspeaker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	pitChannel2Port = 0x42
	pitCommandPort  = 0x43
	pitSquareWave   = 0xB6
	speakerPort     = 0x61
	speakerEnable   = 0x03

	winRing0DriverID   = "WinRing0_1_2_0"
	winRing0DriverFile = "WinRing0x64.sys"
	winRing0Device     = `\\.\WinRing0_1_2_0`

	winRing0DeviceType = 40000
	methodBuffered     = 0
	fileAnyAccess      = 0
	fileReadAccess     = 1
	fileWriteAccess    = 2

	ioctlGetRefCount     = (winRing0DeviceType << 16) | (fileAnyAccess << 14) | (0x801 << 2) | methodBuffered
	ioctlReadIOPortByte  = (winRing0DeviceType << 16) | (fileReadAccess << 14) | (0x833 << 2) | methodBuffered
	ioctlWriteIOPortByte = (winRing0DeviceType << 16) | (fileWriteAccess << 14) | (0x836 << 2) | methodBuffered
)

var speakerMu sync.Mutex

func driverDirectoryRequired() bool { return true }

type winRing0Driver struct {
	handle  windows.Handle
	manager *mgr.Mgr
	service *mgr.Service
	created bool
}

// play implements the WinRing0 service, device, IOCTL, PIT channel-2, and
// speaker-gate sequence directly in Go. No external beep executable or
// WinRing0 DLL is launched or loaded.
func play(ctx context.Context, driverDirectory string, frequencyHz, durationMS uint32) error {
	divisor, err := pitDivisor(frequencyHz)
	if err != nil {
		return err
	}
	driverPath, err := filepath.Abs(filepath.Join(driverDirectory, winRing0DriverFile))
	if err != nil {
		return fmt.Errorf("resolve WinRing0 driver: %w", err)
	}
	if strings.HasPrefix(driverPath, `\\`) {
		return errors.New("WinRing0 driver must be on a local filesystem")
	}
	info, err := os.Stat(driverPath)
	if err != nil {
		return fmt.Errorf("inspect WinRing0 driver %s: %w", driverPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("WinRing0 driver %s is not a regular file", driverPath)
	}

	speakerMu.Lock()
	defer speakerMu.Unlock()
	driver, err := openWinRing0Driver(driverPath)
	if err != nil {
		return err
	}
	defer driver.Close()

	if err := driver.writePortByte(pitCommandPort, pitSquareWave); err != nil {
		return fmt.Errorf("configure PIT channel 2: %w", err)
	}
	if err := driver.writePortByte(pitChannel2Port, byte(divisor)); err != nil {
		return fmt.Errorf("write PIT divisor low byte: %w", err)
	}
	if err := driver.writePortByte(pitChannel2Port, byte(divisor>>8)); err != nil {
		return fmt.Errorf("write PIT divisor high byte: %w", err)
	}
	current, err := driver.readPortByte(speakerPort)
	if err != nil {
		return fmt.Errorf("read PC-speaker gate: %w", err)
	}
	if err := driver.writePortByte(speakerPort, current|speakerEnable); err != nil {
		return fmt.Errorf("enable PC-speaker gate: %w", err)
	}
	defer func() {
		if value, readErr := driver.readPortByte(speakerPort); readErr == nil {
			_ = driver.writePortByte(speakerPort, value&^speakerEnable)
		}
	}()

	timer := time.NewTimer(time.Duration(durationMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openWinRing0Driver(driverPath string) (*winRing0Driver, error) {
	if handle, err := openWinRing0Device(); err == nil {
		return &winRing0Driver{handle: handle}, nil
	}
	manager, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("open Windows service manager for WinRing0: %w", err)
	}
	result := &winRing0Driver{manager: manager, handle: windows.InvalidHandle}
	service, err := manager.OpenService(winRing0DriverID)
	if err != nil {
		if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			manager.Disconnect()
			return nil, fmt.Errorf("open WinRing0 service: %w", err)
		}
		service, err = manager.CreateService(winRing0DriverID, driverPath, mgr.Config{
			DisplayName:  winRing0DriverID,
			ServiceType:  windows.SERVICE_KERNEL_DRIVER,
			StartType:    mgr.StartManual,
			ErrorControl: mgr.ErrorNormal,
		})
		if err != nil {
			manager.Disconnect()
			return nil, fmt.Errorf("install WinRing0 driver service: %w", err)
		}
		result.created = true
	} else if config, configErr := service.Config(); configErr == nil &&
		!strings.EqualFold(filepath.Clean(config.BinaryPathName), filepath.Clean(driverPath)) {
		config.BinaryPathName = driverPath
		config.ServiceType = windows.SERVICE_KERNEL_DRIVER
		config.StartType = mgr.StartManual
		if err := service.UpdateConfig(config); err != nil {
			service.Close()
			manager.Disconnect()
			return nil, fmt.Errorf("update WinRing0 driver path: %w", err)
		}
	}
	result.service = service
	if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		result.Close()
		return nil, fmt.Errorf("start WinRing0 driver service: %w", err)
	}

	var openErr error
	for attempt := 0; attempt < 5; attempt++ {
		result.handle, openErr = openWinRing0Device()
		if openErr == nil {
			return result, nil
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	result.Close()
	return nil, fmt.Errorf("open WinRing0 device after service start: %w", openErr)
}

func openWinRing0Device() (windows.Handle, error) {
	path, err := windows.UTF16PtrFromString(winRing0Device)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		path,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
}

func (driver *winRing0Driver) Close() {
	if driver == nil {
		return
	}
	refCount := uint32(0)
	if driver.handle != windows.InvalidHandle {
		var returned uint32
		output := make([]byte, 4)
		if err := windows.DeviceIoControl(driver.handle, ioctlGetRefCount, nil, 0, &output[0], uint32(len(output)), &returned, nil); err == nil && returned == 4 {
			refCount = binary.LittleEndian.Uint32(output)
		}
		windows.CloseHandle(driver.handle)
		driver.handle = windows.InvalidHandle
	}
	if driver.service != nil {
		if driver.created && refCount <= 1 {
			_, _ = driver.service.Control(svc.Stop)
			_ = driver.service.Delete()
		}
		driver.service.Close()
	}
	if driver.manager != nil {
		driver.manager.Disconnect()
	}
}

func (driver *winRing0Driver) readPortByte(port uint16) (byte, error) {
	input := []byte{byte(port), byte(port >> 8)}
	output := make([]byte, 2)
	var returned uint32
	if err := windows.DeviceIoControl(
		driver.handle, ioctlReadIOPortByte,
		&input[0], uint32(len(input)), &output[0], uint32(len(output)), &returned, nil,
	); err != nil {
		return 0, err
	}
	if returned < 1 {
		return 0, errors.New("WinRing0 returned no I/O-port byte")
	}
	return output[0], nil
}

func (driver *winRing0Driver) writePortByte(port uint16, value byte) error {
	input := winRing0WritePortByteInput(port, value)
	var returned uint32
	return windows.DeviceIoControl(
		driver.handle, ioctlWriteIOPortByte,
		&input[0], uint32(len(input)), nil, 0, &returned, nil,
	)
}

func winRing0WritePortByteInput(port uint16, value byte) []byte {
	// OLS_WRITE_IO_PORT_INPUT is packed on a four-byte boundary and contains a
	// DWORD port plus a DWORD-sized data union. WinRing0's DLL passes the full
	// eight-byte sizeof(struct), even for its byte IOCTL; a five-byte buffer can
	// be accepted by DeviceIoControl yet leave the driver without a complete
	// request on current Windows builds.
	input := make([]byte, 8)
	binary.LittleEndian.PutUint32(input[:4], uint32(port))
	input[4] = value
	return input
}
