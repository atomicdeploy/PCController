// Package winfx contains the small, pointer-width-safe Win32 integration
// surface used by RayanLamp.
//
// The package deliberately does not create windows or own a message loop.
// Callers may therefore use it with Walk or with a raw Win32 window. Functions
// that accept an HWND use uintptr so the UI package does not need to expose a
// particular Win32 binding in its public types.
package winfx
