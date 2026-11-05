//go:build windows

package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"testing"
)

func TestTrayAndMainContextMenusShareStyledPresenter(t *testing.T) {
	shellFile := parseUIFile(t, "shell_windows.go")
	actionsFile := parseUIFile(t, "actions_windows.go")

	for _, route := range []string{"createTray", "windowMessage"} {
		function := findFunction(t, shellFile, route)
		if !callsSelector(function, "s", "showAppMenuAt") {
			t.Errorf("%s no longer routes through Shell.showAppMenuAt", route)
		}
	}
	if callsSelector(findFunction(t, shellFile, "onMouseDown"), "s", "showAppMenuAt") {
		t.Error("main menu is opened on button-down instead of WM_CONTEXTMENU")
	}

	presenter := findFunction(t, actionsFile, "showAppMenuAt")
	if !callsSelector(presenter, "traymenu", "ShowAtAnchor") {
		t.Fatal("shared app-menu presenter does not use the styled traymenu renderer")
	}
	if callsSelector(presenter, "traymenu", "ShowAtCursor") {
		t.Fatal("shared presenter discarded the source-specific screen anchor")
	}
	if !callsSelector(presenter, "win", "IsWindowVisible") {
		t.Fatal("shared presenter no longer derives its action from HWND visibility")
	}
	if !callsSelector(presenter, "win", "IsIconic") {
		t.Fatal("shared presenter no longer treats minimized HWNDs as showable")
	}
	for _, action := range []string{"Show", "Hide"} {
		if !callsSelector(presenter, "s", action) {
			t.Errorf("shared presenter no longer routes the %s action", action)
		}
	}
}

func TestMainWindowVisibilityMenuAction(t *testing.T) {
	tests := []struct {
		name         string
		visible      bool
		wantID       string
		wantLabel    string
		wantShortcut string
		wantIcon     string
	}{
		{
			name:         "hidden window can be shown",
			wantID:       "show",
			wantLabel:    "Show main window",
			wantShortcut: "Open",
			wantIcon:     "icons/menu-window-show.png",
		},
		{
			name:         "visible window can be hidden",
			visible:      true,
			wantID:       "hide",
			wantLabel:    "Hide main window",
			wantShortcut: "Hide",
			wantIcon:     "icons/menu-window-hide.png",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			id, label, shortcut, iconPath :=
				mainWindowVisibilityMenuAction(test.visible)
			if id != test.wantID ||
				label != test.wantLabel ||
				shortcut != test.wantShortcut ||
				iconPath != test.wantIcon {
				t.Fatalf(
					"action = (%q, %q, %q, %q), want (%q, %q, %q, %q)",
					id,
					label,
					shortcut,
					iconPath,
					test.wantID,
					test.wantLabel,
					test.wantShortcut,
					test.wantIcon,
				)
			}
		})
	}
}

func TestMainWindowVisibilityActionIsImmediatelyAboveExit(t *testing.T) {
	file := parseUIFile(t, "actions_windows.go")
	function := findFunction(t, file, "showAppMenuAt")
	order := appMenuItemOrder(t, function)

	visibility, quit := -1, -1
	for index, id := range order {
		switch id {
		case "visibility":
			visibility = index
		case "quit":
			quit = index
		}
	}
	if visibility < 0 || quit < 0 {
		t.Fatalf("shared menu order is missing visibility or quit: %v", order)
	}
	if visibility+1 != quit {
		t.Fatalf(
			"visibility action index=%d, exit index=%d; want visibility immediately above exit",
			visibility,
			quit,
		)
	}
}

func parseUIFile(t *testing.T, name string) *ast.File {
	t.Helper()
	source, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}

func findFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("function %s not found", name)
	return nil
}

func callsSelector(function *ast.FuncDecl, receiver, method string) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == receiver {
			found = true
		}
		return true
	})
	return found
}

func appMenuItemOrder(t *testing.T, function *ast.FuncDecl) []string {
	t.Helper()
	var order []string
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
			return true
		}
		name, ok := assignment.Lhs[0].(*ast.Ident)
		if !ok || name.Name != "items" {
			return true
		}
		literal, ok := assignment.Rhs[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range literal.Elts {
			call, ok := element.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				continue
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || callee.Name != "item" {
				continue
			}
			switch first := call.Args[0].(type) {
			case *ast.Ident:
				if first.Name == "visibilityID" {
					order = append(order, "visibility")
				}
			case *ast.BasicLit:
				if first.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(first.Value)
				if err == nil {
					order = append(order, value)
				}
			}
		}
		return false
	})
	return order
}
