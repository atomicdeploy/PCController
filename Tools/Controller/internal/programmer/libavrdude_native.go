//go:build cgo && libavrdude

package programmer

/*
#include <stdlib.h>
#include "../../../AvrdudeAdapter/bridge.h"
int pcc_go_avr_run(int argc, char **argv, uintptr_t user);
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"runtime/cgo"
	"strings"
	"unsafe"
)

const nativeAvrdudeAvailable = true

// Covers configuration, callbacks, programmer state and cleanup across every
// board/client in this process. A canceled waiter must never start a C call.
var nativeAvrdudeGate = make(chan struct{}, 1)

type nativeAvrdudeCall struct {
	ctx         context.Context
	output      io.Writer
	stage       string
	resultOK    bool
	outputError error
}

//export pccGoAvrCancelled
func pccGoAvrCancelled(user C.uintptr_t) C.int {
	call := cgo.Handle(user).Value().(*nativeAvrdudeCall)
	if call.ctx.Err() != nil || call.outputError != nil {
		return 1
	}
	return 0
}

//export pccGoAvrEvent
func pccGoAvrEvent(user C.uintptr_t, kind *C.char, value C.int, message *C.char) {
	call := cgo.Handle(user).Value().(*nativeAvrdudeCall)
	text := C.GoString(message)
	switch C.GoString(kind) {
	case "progress":
		stage := call.stage
		if text != "" {
			stage += ":" + strings.ToLower(strings.TrimSuffix(text, " ..."))
		}
		ReportProgress(call.ctx, Progress{Stage: stage, Percent: int(value), Detail: text})
	case "message":
		if call.outputError == nil {
			_, call.outputError = io.WriteString(call.output, text)
		}
	case "error":
		if call.outputError == nil {
			_, call.outputError = fmt.Fprintln(call.output, text)
		}
	case "result":
		call.resultOK = value != 0
	}
}

func runNativeAvrdude(ctx context.Context, command Command, output io.Writer) error {
	select {
	case nativeAvrdudeGate <- struct{}{}:
		defer func() { <-nativeAvrdudeGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	call := &nativeAvrdudeCall{ctx: ctx, output: output, stage: commandStage(command)}
	handle := cgo.NewHandle(call)
	defer handle.Delete()
	args := append([]string{"pccontroller"}, command.Args...)
	// argv and strings live in C memory for the complete synchronous operation.
	argv := C.malloc(C.size_t(len(args)) * C.size_t(unsafe.Sizeof(uintptr(0))))
	if argv == nil {
		return fmt.Errorf("allocate libavrdude arguments")
	}
	defer C.free(argv)
	vector := unsafe.Slice((**C.char)(argv), len(args))
	for i, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("libavrdude argument contains NUL")
		}
		vector[i] = C.CString(arg)
		defer C.free(unsafe.Pointer(vector[i]))
	}
	ReportProgress(ctx, Progress{Stage: call.stage, Percent: -1})
	status := C.pcc_go_avr_run(C.int(len(args)), (**C.char)(argv), C.uintptr_t(handle))
	// Never return on a separate goroutine while C still owns the programmer.
	// Native transport calls may delay cancellation until their own timeout.
	if err := ctx.Err(); err != nil {
		return err
	}
	if call.outputError != nil {
		return call.outputError
	}
	if status != 0 || !call.resultOK {
		return fmt.Errorf("embedded libavrdude failed during %s (status %d)", call.stage, int(status))
	}
	return nil
}
