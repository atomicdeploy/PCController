package appconfig

import (
	"fmt"
	"sort"
	"strings"
)

var RFCategoryPalette = []string{"red", "blue", "violet", "green", "white"}

type RFConfig struct {
	DisplayRadix string       `json:"display_radix"`
	Categories   []RFCategory `json:"categories,omitempty"`
	Metadata     []RFMetadata `json:"metadata,omitempty"`
}

type RFCategory struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type RFCodeKey struct {
	Code     uint32 `json:"code"`
	Bits     byte   `json:"bits"`
	Protocol byte   `json:"protocol"`
}

func (key RFCodeKey) StableKey() string {
	return fmt.Sprintf("%08X:%02X:%02X", key.Code, key.Bits, key.Protocol)
}

type RFMetadata struct {
	Key      RFCodeKey `json:"key"`
	Name     string    `json:"name,omitempty"`
	Category string    `json:"category,omitempty"`
}

func DefaultRFConfig() RFConfig {
	return RFConfig{DisplayRadix: "hex"}
}

func FormatRFCode(code uint32, radix string) string {
	if strings.EqualFold(strings.TrimSpace(radix), "decimal") {
		return fmt.Sprintf("%d", code)
	}
	return fmt.Sprintf("0x%08X", code)
}

func (config RFConfig) MetadataFor(key RFCodeKey) (RFMetadata, bool) {
	stable := key.StableKey()
	for _, metadata := range config.Metadata {
		if metadata.Key.StableKey() == stable {
			return metadata, true
		}
	}
	return RFMetadata{}, false
}

func validateRFConfig(config RFConfig) error {
	radix := strings.ToLower(strings.TrimSpace(config.DisplayRadix))
	if radix != "hex" && radix != "decimal" {
		return fmt.Errorf("rf.display_radix must be hex or decimal")
	}
	categoryNames := make(map[string]bool)
	for index, category := range config.Categories {
		name := strings.ToLower(strings.TrimSpace(category.Name))
		if name == "" || categoryNames[name] {
			return fmt.Errorf("rf.categories[%d].name is required and must be unique", index)
		}
		categoryNames[name] = true
		color := strings.ToLower(strings.TrimSpace(category.Color))
		if color == "purple" {
			color = "violet"
		}
		if !stringInSlice(color, RFCategoryPalette) {
			return fmt.Errorf("rf.categories[%d].color must be red, blue, violet, green, or white", index)
		}
	}
	keys := make(map[string]bool)
	for index, metadata := range config.Metadata {
		if metadata.Key.Code == 0 || metadata.Key.Bits == 0 || metadata.Key.Bits > 32 ||
			metadata.Key.Protocol == 0 || metadata.Key.Protocol > 12 {
			return fmt.Errorf("rf.metadata[%d].key has invalid code/bits/protocol", index)
		}
		key := metadata.Key.StableKey()
		if keys[key] {
			return fmt.Errorf("rf.metadata[%d].key %s is duplicated", index, key)
		}
		keys[key] = true
		if len(metadata.Name) > 64 || !printableText(metadata.Name) {
			return fmt.Errorf("rf.metadata[%d].name must be at most 64 printable characters", index)
		}
		if metadata.Category != "" && !categoryNames[strings.ToLower(strings.TrimSpace(metadata.Category))] {
			return fmt.Errorf("rf.metadata[%d].category %q is undefined", index, metadata.Category)
		}
	}
	return nil
}

func cloneRFConfig(source RFConfig) RFConfig {
	result := source
	result.Categories = append([]RFCategory(nil), source.Categories...)
	result.Metadata = append([]RFMetadata(nil), source.Metadata...)
	return result
}

func canonicalizeRFConfig(source RFConfig) RFConfig {
	result := cloneRFConfig(source)
	result.DisplayRadix = strings.ToLower(strings.TrimSpace(result.DisplayRadix))
	for index := range result.Categories {
		result.Categories[index].Name = strings.TrimSpace(result.Categories[index].Name)
		color := strings.ToLower(strings.TrimSpace(result.Categories[index].Color))
		if color == "purple" {
			color = "violet"
		}
		result.Categories[index].Color = color
	}
	for index := range result.Metadata {
		result.Metadata[index].Name = strings.TrimSpace(result.Metadata[index].Name)
		result.Metadata[index].Category = strings.TrimSpace(result.Metadata[index].Category)
	}
	return result
}

func SortedRFMetadata(values []RFMetadata) []RFMetadata {
	result := append([]RFMetadata(nil), values...)
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key.StableKey() < result[j].Key.StableKey()
	})
	return result
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
