package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestNativeCopyDoesNotTranslateEnteredNamesOrTechnicalPayloads(t *testing.T) {
	fa := New(Persian)
	en := New(English)
	if fa.NativeText("Save step") == "Save step" || en.NativeText("Save step") != "Save step" {
		t.Fatal("native language not applied")
	}
	for _, value := range []string{"Scene 1", "effect:1", "pwm.1", "pccontroller://127.0.0.1:8787", `{"kind":"relay"}`} {
		if fa.NativeText(value) != value {
			t.Fatalf("technical/user value changed: %s", value)
		}
	}
	if fa.NativeText("Sequence editor · Scene 1") != "ویرایشگر توالی · Scene 1" {
		t.Fatal("author's effect title was translated or digit-shaped")
	}
}

func TestNativeAdapterStaticLabelsHavePersianCopy(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "Controller", "cmd", "controller")
	files, err := filepath.Glob(filepath.Join(root, "native_*windows.go"))
	if err != nil || len(files) == 0 {
		t.Fatal("native adapter sources unavailable", err)
	}
	patterns := []*regexp.Regexp{regexp.MustCompile(`(?:Label|label|Title)\s*:\s*"([^"]+)"`), regexp.MustCompile(`(?:field|choice|nativeBool|number|nativeNumber)\("[^"]+",\s*"([^"]+)"`), regexp.MustCompile(`return\s+"([^"]+)",\s*\[\]nativeToolOperation`)}
	fa := New(Persian)
	missing := map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
				label := match[1]
				if strings.TrimSpace(label) != "" && fa.NativeText(label) == label {
					missing[label] = true
				}
			}
		}
	}
	for label := range missing {
		t.Errorf("untranslated native label: %s", label)
	}
}
