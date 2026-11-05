//go:build windows

package headlessweb

import (
	"strings"
	"testing"
)

func TestParseLaunchOptions(t *testing.T) {
	t.Parallel()
	options, err := ParseLaunchOptions([]string{
		"--no-open",
		"--config", `C:\Temp\controller.json`,
		"--listen", "localhost:49123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.Open ||
		options.ConfigPath != `C:\Temp\controller.json` ||
		options.Listen != "localhost:49123" {
		t.Fatalf("options = %#v", options)
	}

	defaults, err := ParseLaunchOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !defaults.Open {
		t.Fatal("serve should open the browser by default")
	}
}

func TestParseLaunchOptionsRejectsOperands(t *testing.T) {
	t.Parallel()
	if _, err := ParseLaunchOptions([]string{"dashboard"}); err == nil {
		t.Fatal("unexpected serve operand was accepted")
	}
}

func TestUsageExplainsNativeSurfaceBoundary(t *testing.T) {
	t.Parallel()
	usage := Usage()
	for _, required := range []string{
		"PCControllerDesktop.exe serve",
		"--no-open",
		"--config PATH",
		"--listen HOST:PORT",
		"no native window",
	} {
		if !strings.Contains(usage, required) {
			t.Errorf("usage does not contain %q", required)
		}
	}
}
