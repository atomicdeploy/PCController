//go:build windows

package nativeshell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const (
	wmPowerBroadcast   = 0x0218
	wmWTSSessionChange = 0x02B1

	wtsSessionLock   = 0x7
	wtsSessionUnlock = 0x8

	pbtAPMSuspend         = 0x4
	pbtAPMResumeCritical  = 0x6
	pbtAPMResumeSuspend   = 0x7
	pbtAPMResumeAutomatic = 0x12

	notifyForThisSession = 0
	networkPollInterval  = 2 * time.Second
	networkStopWait      = 250 * time.Millisecond
)

var (
	wtsapi32                             = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSRegisterSessionNotification   = wtsapi32.NewProc("WTSRegisterSessionNotification")
	procWTSUnregisterSessionNotification = wtsapi32.NewProc("WTSUnRegisterSessionNotification")
)

func decodeNativeSystemEvent(message uint32, wParam uintptr) (SystemEvent, bool) {
	switch message {
	case wmWTSSessionChange:
		switch wParam {
		case wtsSessionLock:
			return SystemEvent{Kind: SystemEventSessionLocked}, true
		case wtsSessionUnlock:
			return SystemEvent{Kind: SystemEventSessionUnlocked}, true
		}
	case wmPowerBroadcast:
		switch wParam {
		case pbtAPMSuspend:
			return SystemEvent{Kind: SystemEventPowerSuspending}, true
		case pbtAPMResumeCritical, pbtAPMResumeSuspend, pbtAPMResumeAutomatic:
			return SystemEvent{Kind: SystemEventPowerResumed}, true
		}
	}
	return SystemEvent{}, false
}

type networkSnapshot struct {
	signature      string
	interfaceCount int
	addressCount   int
}

type networkChangeState struct {
	previous networkSnapshot
	have     bool
}

func (state *networkChangeState) observe(next networkSnapshot) (SystemEvent, bool) {
	if !state.have {
		state.previous = next
		state.have = true
		return SystemEvent{}, false
	}
	if next.signature == state.previous.signature {
		return SystemEvent{}, false
	}
	state.previous = next
	return SystemEvent{
		Kind:             SystemEventNetworkChanged,
		NetworkSignature: next.signature,
		InterfaceCount:   next.interfaceCount,
		AddressCount:     next.addressCount,
	}, true
}

func captureNetworkSnapshot() (networkSnapshot, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return networkSnapshot{}, fmt.Errorf("enumerate host network interfaces: %w", err)
	}
	sort.Slice(interfaces, func(left, right int) bool {
		if interfaces[left].Index != interfaces[right].Index {
			return interfaces[left].Index < interfaces[right].Index
		}
		return interfaces[left].Name < interfaces[right].Name
	})
	var normalized strings.Builder
	addressCount := 0
	for _, networkInterface := range interfaces {
		normalized.WriteString(strconv.Itoa(networkInterface.Index))
		normalized.WriteByte('|')
		normalized.WriteString(networkInterface.Name)
		normalized.WriteByte('|')
		normalized.WriteString(strconv.Itoa(networkInterface.MTU))
		normalized.WriteByte('|')
		normalized.WriteString(strconv.FormatUint(uint64(networkInterface.Flags), 10))
		normalized.WriteByte('|')
		normalized.WriteString(networkInterface.HardwareAddr.String())
		normalized.WriteByte('|')
		addresses, addressErr := networkInterface.Addrs()
		if addressErr != nil {
			return networkSnapshot{}, fmt.Errorf(
				"enumerate addresses for network interface %d: %w",
				networkInterface.Index,
				addressErr,
			)
		}
		normalizedAddresses := make([]string, 0, len(addresses))
		for _, address := range addresses {
			normalizedAddresses = append(normalizedAddresses, address.String())
		}
		sort.Strings(normalizedAddresses)
		addressCount += len(normalizedAddresses)
		for _, address := range normalizedAddresses {
			normalized.WriteString(address)
			normalized.WriteByte(',')
		}
		normalized.WriteByte('\n')
	}
	digest := sha256.Sum256([]byte(normalized.String()))
	return networkSnapshot{
		signature:      hex.EncodeToString(digest[:16]),
		interfaceCount: len(interfaces),
		addressCount:   addressCount,
	}, nil
}

func (shell *windowsShell) initializeSystemEvents(hwnd uintptr) {
	if shell.options.HandleSystemEvent == nil {
		return
	}
	if err := procWTSRegisterSessionNotification.Find(); err != nil {
		report(shell.options, fmt.Errorf("load Windows session notifications: %w", err))
	} else if result, _, callErr := procWTSRegisterSessionNotification.Call(
		hwnd,
		notifyForThisSession,
	); result == 0 {
		report(shell.options, windowsCallError("register Windows session notifications", callErr))
	} else {
		shell.sessionNotifications = true
	}
	shell.startNetworkMonitor()
}

func (shell *windowsShell) unregisterSystemEvents(hwnd uintptr) {
	if !shell.sessionNotifications || hwnd == 0 {
		return
	}
	shell.sessionNotifications = false
	if err := procWTSUnregisterSessionNotification.Find(); err != nil {
		return
	}
	_, _, _ = procWTSUnregisterSessionNotification.Call(hwnd)
}

func (shell *windowsShell) startNetworkMonitor() {
	done := make(chan struct{})
	shell.networkMonitorDone = done
	go func(ctx context.Context) {
		defer close(done)
		ticker := time.NewTicker(networkPollInterval)
		defer ticker.Stop()
		var changes networkChangeState
		lastError := ""
		sample := func() {
			snapshot, err := captureNetworkSnapshot()
			if err != nil {
				message := err.Error()
				if message != lastError {
					report(shell.options, err)
					lastError = message
				}
				return
			}
			lastError = ""
			event, changed := changes.observe(snapshot)
			if !changed {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				shell.systemEvents.emit(event)
			}
		}
		sample()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}(shell.actionContext)
}

func (shell *windowsShell) waitForNetworkMonitor() {
	done := shell.networkMonitorDone
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(networkStopWait):
	}
}
