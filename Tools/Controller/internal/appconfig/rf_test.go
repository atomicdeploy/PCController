package appconfig

import "testing"

func TestRFMetadataUsesStableCodeBitsProtocolTuple(t *testing.T) {
	config := DefaultRFConfig()
	config.Categories = []RFCategory{{Name: "Motion", Color: "red"}}
	config.Metadata = []RFMetadata{{
		Key:  RFCodeKey{Code: 0xABCDEF, Bits: 24, Protocol: 1},
		Name: "Side A Up", Category: "Motion",
	}}
	if err := validateRFConfig(config); err != nil {
		t.Fatal(err)
	}
	metadata, ok := config.MetadataFor(RFCodeKey{Code: 0xABCDEF, Bits: 24, Protocol: 1})
	if !ok || metadata.Name != "Side A Up" {
		t.Fatalf("metadata=%#v found=%t", metadata, ok)
	}
	if _, ok := config.MetadataFor(RFCodeKey{Code: 0xABCDEF, Bits: 24, Protocol: 2}); ok {
		t.Fatal("protocol change incorrectly matched metadata")
	}
}

func TestRFDisplayRadixAndPalette(t *testing.T) {
	if got := FormatRFCode(0xABCDEF, "hex"); got != "0x00ABCDEF" {
		t.Fatalf("hex=%q", got)
	}
	if got := FormatRFCode(0xABCDEF, "decimal"); got != "11259375" {
		t.Fatalf("decimal=%q", got)
	}
	config := DefaultRFConfig()
	if len(config.Categories) != 0 {
		t.Fatalf("default categories must be user-defined: %#v", config.Categories)
	}
	for index, expected := range RFCategoryPalette {
		if []string{"red", "blue", "violet", "green", "white"}[index] != expected {
			t.Fatalf("palette order=%#v", RFCategoryPalette)
		}
	}
}
