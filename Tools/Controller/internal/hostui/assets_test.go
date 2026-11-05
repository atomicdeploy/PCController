package hostui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/controller/winres"
)

func TestToastAssetsWorkWithoutPackagePNGsAndPreferLocalOverride(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "controller.exe")
	for _, name := range append([]string{ToastLogoFileName}, func() []string {
		var names []string
		for _, icon := range toastEventIconNames {
			names = append(names, toastEventIconFileName(icon))
		}
		return names
	}()...) {
		t.Run(name, func(t *testing.T) {
			path, err := resolveToastAsset(executable, name)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			embeddedName := name
			if name == ToastLogoFileName {
				embeddedName = "icon.png"
			}
			want, err := winres.ToastImages.ReadFile(embeddedName)
			if err != nil || !bytes.Equal(data, want) {
				t.Fatalf("built-in image mismatch: %v", err)
			}
			override := filepath.Join(filepath.Dir(executable), "assets", "toast", name)
			if err := os.MkdirAll(filepath.Dir(override), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(override, want, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := resolveToastAsset(executable, name)
			if err != nil || got != override {
				t.Fatalf("override=%s error=%v", got, err)
			}
		})
	}
}
