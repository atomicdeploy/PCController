package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// A source-level guard is deliberate: runReset owns process-wide IPC/serial
// arbitration. Do not open a real USB device just to check this call ordering.
func TestStandaloneResetDoesNotRequireResponsiveFirmware(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "device_cli.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reset *ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "runReset" {
			reset = function
		}
	}
	if reset == nil {
		t.Fatal("reset command missing")
	}
	pulsed, reauthenticated := false, false
	ast.Inspect(reset.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "EnsureConnected", "Authenticate", "AuthenticateWithRetry":
			t.Error("reset must not depend on a pre-reset firmware handshake")
		case "PulseResetFor":
			pulsed = true
		case "Reconnect":
			if !pulsed {
				t.Error("reauthentication precedes reset")
			}
			reauthenticated = true
		}
		return true
	})
	if !pulsed || !reauthenticated {
		t.Fatal("reset must pulse then reauthenticate before claiming recovery")
	}
}
