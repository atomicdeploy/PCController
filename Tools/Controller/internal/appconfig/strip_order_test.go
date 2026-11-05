package appconfig

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestStripOrderDefaultsValidationAndPersistence(t *testing.T) {
	value := Defaults()
	if value.UI.StripColorOrder != "BRG" || value.UI.StripFirmwareOrder != "BRG" {
		t.Fatal("changed legacy wire order")
	}
	if !reflect.DeepEqual(StripColorOrders(), []string{"GRB", "RGB", "BRG", "BGR", "RBG", "GBR"}) {
		t.Fatal("common modes must come first")
	}
	for _, order := range StripColorOrders() {
		value.UI.StripColorOrder = order
		if err := value.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	value.UI.StripColorOrder = "GRB"
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(path)
	if err != nil || loaded.UI.StripColorOrder != "GRB" {
		t.Fatalf("reload: %v", err)
	}
	value.UI.StripColorOrder = "RRG"
	if value.Validate() == nil {
		t.Fatal("accepted duplicated channel")
	}
	value.UI.StripColorOrder = "RGB"
	value.UI.StripFirmwareOrder = "invalid"
	if value.Validate() == nil {
		t.Fatal("accepted invalid firmware encoder")
	}
}
