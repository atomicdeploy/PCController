package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProductionIPCCompositionRootsEnableAlphaAuthorizationMode(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve authorization contract test path")
	}
	controllerDir := filepath.Dir(testFile)
	tests := []struct {
		name     string
		path     string
		function string
	}{
		{name: "primary", path: filepath.Join(controllerDir, "primary.go"), function: "startPrimaryIPCAtWithIdentity"},
		{name: "device CLI", path: filepath.Join(controllerDir, "device_cli.go"), function: "runIPC"},
		{name: "host bridge", path: filepath.Join(controllerDir, "..", "..", "internal", "hostbridge", "manager.go"), function: "remotePeerService"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parser.ParseFile(token.NewFileSet(), test.path, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			var function *ast.FuncDecl
			for _, declaration := range parsed.Decls {
				candidate, ok := declaration.(*ast.FuncDecl)
				if ok && candidate.Name.Name == test.function {
					function = candidate
					break
				}
			}
			if function == nil {
				t.Fatalf("production composition root %s was not found", test.function)
			}

			serviceLiterals := 0
			alphaLiterals := 0
			ast.Inspect(function.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				selector, ok := literal.Type.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				packageName, ok := selector.X.(*ast.Ident)
				if !ok || packageName.Name != "ipcjson" || selector.Sel.Name != "Service" {
					return true
				}
				serviceLiterals++
				for _, element := range literal.Elts {
					field, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, keyOK := field.Key.(*ast.Ident)
					value, valueOK := field.Value.(*ast.Ident)
					if keyOK && valueOK && key.Name == "AuthorizationDisabled" && value.Name == "true" {
						alphaLiterals++
					}
				}
				return true
			})
			if serviceLiterals != 1 || alphaLiterals != serviceLiterals {
				t.Fatalf(
					"%s contains %d ipcjson.Service composition roots, %d explicitly enable alpha authorization mode",
					test.function, serviceLiterals, alphaLiterals,
				)
			}
		})
	}
}
