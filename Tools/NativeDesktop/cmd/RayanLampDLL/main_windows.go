//go:build windows && cgo && rayanlamp_sdk

package main

/*
#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"rayanlamp.local/app/internal/dllapi"
)

var panicEnvelope = []byte(
	`{"ok":false,"error":{"code":"internal_error","message":"RayanLamp SDK bridge recovered from an internal failure"}}`,
)

//export RayanLampGoABIVersion
func RayanLampGoABIVersion() C.uint32_t {
	return C.uint32_t(dllapi.ABIVersion)
}

//export RayanLampGoIsRunning
func RayanLampGoIsRunning(
	timeoutMilliseconds C.uint32_t,
) (result C.int32_t) {
	result = C.int32_t(dllapi.StatusInternal)
	defer func() {
		if recover() != nil {
			result = C.int32_t(dllapi.StatusInternal)
		}
	}()
	return C.int32_t(dllapi.IsRunning(uint32(timeoutMilliseconds)))
}

//export RayanLampGoExecute
func RayanLampGoExecute(
	method *C.char,
	methodLength C.size_t,
	params *C.char,
	paramsLength C.size_t,
	timeoutMilliseconds C.uint32_t,
	output **C.char,
	outputLength *C.size_t,
) (result C.int32_t) {
	result = C.int32_t(dllapi.StatusInternal)
	if output == nil || outputLength == nil {
		return C.int32_t(dllapi.StatusInvalidArgument)
	}
	*output = nil
	*outputLength = 0

	defer func() {
		if recover() == nil {
			return
		}
		_ = writeOutput(panicEnvelope, output, outputLength)
		result = C.int32_t(dllapi.StatusInternal)
	}()

	methodBytes, ok := copyInput(method, methodLength)
	if !ok {
		status, response := invalidInputResponse()
		if !writeOutput(response, output, outputLength) {
			return C.int32_t(dllapi.StatusInternal)
		}
		return C.int32_t(status)
	}
	paramsBytes, ok := copyInput(params, paramsLength)
	if !ok {
		status, response := invalidInputResponse()
		if !writeOutput(response, output, outputLength) {
			return C.int32_t(dllapi.StatusInternal)
		}
		return C.int32_t(status)
	}
	status, response := dllapi.Execute(
		methodBytes,
		paramsBytes,
		uint32(timeoutMilliseconds),
	)
	if !writeOutput(response, output, outputLength) {
		return C.int32_t(dllapi.StatusInternal)
	}
	return C.int32_t(status)
}

func copyInput(pointer *C.char, length C.size_t) ([]byte, bool) {
	if uint64(length) > dllapi.MaxInputBytes {
		return nil, false
	}
	if length == 0 {
		return nil, true
	}
	if pointer == nil {
		return nil, false
	}
	return C.GoBytes(unsafe.Pointer(pointer), C.int(length)), true
}

func invalidInputResponse() (int32, []byte) {
	return dllapi.Execute(nil, nil, 0)
}

func writeOutput(
	data []byte,
	output **C.char,
	outputLength *C.size_t,
) bool {
	if output == nil || outputLength == nil {
		return false
	}
	if len(data) > dllapi.MaxInputBytes {
		return false
	}
	allocation := C.malloc(C.size_t(len(data) + 1))
	if allocation == nil {
		return false
	}
	buffer := unsafe.Slice((*byte)(allocation), len(data)+1)
	copy(buffer, data)
	buffer[len(data)] = 0
	*output = (*C.char)(allocation)
	*outputLength = C.size_t(len(data))
	return true
}

func main() {}
