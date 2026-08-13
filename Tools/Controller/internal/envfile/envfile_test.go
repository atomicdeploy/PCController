package envfile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAcceptsQuotedAndExportedValues(t *testing.T) {
	values, err := parse("ALPHA=one\t # comment\nexport\tBETA=\"two words\"\nGAMMA='three # literal'\n", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := values["ALPHA"], "one"; got != want {
		t.Fatalf("ALPHA=%q want %q", got, want)
	}
	if got, want := values["BETA"], "two words"; got != want {
		t.Fatalf("BETA=%q want %q", got, want)
	}
	if got, want := values["GAMMA"], "three # literal"; got != want {
		t.Fatalf("GAMMA=%q want %q", got, want)
	}
}

func TestLoadProcessRejectsMissingExplicitFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.env")
	t.Setenv("PCCONTROLLER_ENV_FILE", missing)
	_, err := LoadProcess()
	if err == nil || !strings.Contains(err.Error(), "read explicit environment file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProcessCanonicalizesRelativeExplicitFileForChildren(t *testing.T) {
	directory := t.TempDir()
	child := filepath.Join(directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "shared.env")
	if err := os.WriteFile(file, []byte("ENVFILE_CHILD_TEST=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("PCCONTROLLER_ENV_FILE", "shared.env")
	result, err := LoadProcess()
	if err != nil {
		t.Fatal(err)
	}
	selector := os.Getenv("PCCONTROLLER_ENV_FILE")
	resultInfo, resultInfoErr := os.Stat(result.Path)
	wantInfo, wantInfoErr := os.Stat(file)
	if result.Path != selector || resultInfoErr != nil || wantInfoErr != nil || !os.SameFile(resultInfo, wantInfo) {
		t.Fatalf("result=%#v selector=%q want %q", result, os.Getenv("PCCONTROLLER_ENV_FILE"), file)
	}
	path, err := projectFile(child, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	childInfo, childInfoErr := os.Stat(path)
	if childInfoErr != nil || !os.SameFile(childInfo, wantInfo) {
		t.Fatalf("child resolved %q want %q", path, file)
	}
}

func TestGoToolEntrypointsLoadTheRepositoryEnvironmentContract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	controllerRoot := filepath.Join(root, "Tools", "Controller")
	discovered := 0
	err = filepath.WalkDir(controllerRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".build", ".cache", "bin", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		if file.Name.Name != "main" {
			return nil
		}
		hasMain := false
		hasLoad := false
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.FuncDecl:
				hasMain = hasMain || (value.Recv == nil && value.Name.Name == "main")
			case *ast.CallExpr:
				selector, ok := value.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, identifierOK := selector.X.(*ast.Ident)
				hasLoad = hasLoad || (identifierOK && identifier.Name == "envfile" && selector.Sel.Name == "LoadProcess")
			}
			return true
		})
		if !hasMain {
			return nil
		}
		discovered++
		if !hasLoad {
			t.Errorf("%s must load the repository environment contract", filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if discovered == 0 {
		t.Fatal("no Go tool entrypoints were discovered")
	}
}

func TestParseRejectsMalformedAssignment(t *testing.T) {
	_, err := parse("NO_EQUALS\n", "fixture.env")
	if err == nil || !strings.Contains(err.Error(), "fixture.env:1: expected KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProjectFileDoesNotSearchPastAStandaloneWorkingDirectory(t *testing.T) {
	directory := t.TempDir()
	path, err := projectFile(directory, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(directory, ".env"); path != want {
		t.Fatalf("path=%q want %q", path, want)
	}
}

func TestLoadProcessAppliesFileWithoutOverridingInheritedValues(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".env"), []byte("ENVFILE_TEST_INHERITED=file\nENVFILE_TEST_VALUE=loaded\nENVFILE_TEST_APPLIED=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("PCCONTROLLER_ENV_FILE", "")
	t.Setenv("ENVFILE_TEST_INHERITED", "process")
	t.Setenv("ENVFILE_TEST_VALUE", "")
	result, err := LoadProcess()
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(directory, ".env")
	gotInfo, gotInfoErr := os.Stat(result.Path)
	wantInfo, wantInfoErr := os.Stat(wantPath)
	if !result.Loaded || gotInfoErr != nil || wantInfoErr != nil || !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got, want := os.Getenv("ENVFILE_TEST_INHERITED"), "process"; got != want {
		t.Fatalf("inherited=%q want %q", got, want)
	}
	if got, want := os.Getenv("ENVFILE_TEST_VALUE"), ""; got != want {
		t.Fatalf("empty inherited value must win, got %q", got)
	}
	if got, want := os.Getenv("ENVFILE_TEST_APPLIED"), "yes"; got != want {
		t.Fatalf("applied=%q want %q", got, want)
	}
}
