//go:build windows

package comserver

import (
	"context"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"rayanlamp.local/app/internal/ipc"
)

func TestNativeVTablesInitialize(t *testing.T) {
	if err := initializeNativeTables(); err != nil {
		t.Fatal(err)
	}
	if nativeTables.factory == 0 || nativeTables.dispatch == 0 {
		t.Fatalf("invalid vtables: factory=%#x dispatch=%#x", nativeTables.factory, nativeTables.dispatch)
	}
}

func TestVariantStringRoundTrip(t *testing.T) {
	var value variant
	if code := setVariant(&value, "چراغ روشن"); code != sOK {
		t.Fatalf("setVariant HRESULT = %#x", code)
	}
	if value.VT != vtBSTR {
		t.Fatalf("variant type = %d", value.VT)
	}
	defer freeBSTR(uintptr(value.Value))
	got, err := variantAsString(&value)
	if err != nil {
		t.Fatal(err)
	}
	if got != "چراغ روشن" {
		t.Fatalf("value = %q", got)
	}
}

func TestNativeStructLayouts(t *testing.T) {
	if got := unsafe.Sizeof(variant{}); got != 16 {
		t.Fatalf("VARIANT size = %d, want 16", got)
	}
	pointerSize := unsafe.Sizeof(uintptr(0))
	wantParams := uintptr(16)
	wantException := uintptr(32)
	if pointerSize == 8 {
		wantParams = 24
		wantException = 64
	}
	if got := unsafe.Sizeof(dispatchParams{}); got != wantParams {
		t.Fatalf("DISPPARAMS size = %d, want %d", got, wantParams)
	}
	if got := unsafe.Sizeof(exceptionInfo{}); got != wantException {
		t.Fatalf("EXCEPINFO size = %d, want %d", got, wantException)
	}
}

func TestHideHasNoAutomationArguments(t *testing.T) {
	count, known := expectedArgumentCount(dispIDHide)
	if !known || count != 0 {
		t.Fatalf(
			"Hide argument contract = (%d, %t), want (0, true)",
			count,
			known,
		)
	}
}

func TestNativeDispatchInvoke(t *testing.T) {
	if err := initializeNativeTables(); err != nil {
		t.Fatal(err)
	}
	var captured ipc.Request
	automationBridge, err := newBridge(func(_ context.Context, request ipc.Request) (any, error) {
		captured = request
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &nativeServer{ctx: context.Background(), bridge: automationBridge}
	identity, err := newDispatch(server)
	if err != nil {
		t.Fatal(err)
	}
	defer dispatchRelease(identity)

	bstr, err := allocateBSTR("hello from COM")
	if err != nil {
		t.Fatal(err)
	}
	defer freeBSTR(bstr)
	arguments := []variant{{VT: vtBSTR, Value: uint64(bstr)}}
	params := dispatchParams{Args: &arguments[0], ArgCount: 1}
	var result variant
	code, _, _ := syscall.SyscallN(
		nativeTables.dispatchEntries[6],
		identity,
		uintptr(uint32(dispIDNotify)),
		uintptr(unsafe.Pointer(&iidNull)),
		0,
		dispatchMethod,
		uintptr(unsafe.Pointer(&params)),
		uintptr(unsafe.Pointer(&result)),
		0,
		0,
	)
	if hresultFailed(code) {
		t.Fatalf("Invoke HRESULT = %#x", code)
	}
	if captured.Method != "notifications.show" {
		t.Fatalf("method = %q", captured.Method)
	}
	assertDispatchJSON(t, captured.Params, `{"message":"hello from COM"}`)
}

func TestRunStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Run(ctx, func(context.Context, ipc.Request) (any, error) {
		t.Fatal("handler unexpectedly called")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}
