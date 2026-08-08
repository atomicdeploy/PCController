package productidentity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedIdentityMatchesCanonicalPackageMetadata(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "web", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Version                   string `json:"version"`
		ProductName               string `json:"productName"`
		ProductShortName          string `json:"productShortName"`
		ProductTagline            string `json:"productTagline"`
		Description               string `json:"description"`
		ProductAppID              string `json:"productAppId"`
		ProductProtocol           string `json:"productProtocol"`
		ProductConfigDirectory    string `json:"productConfigDirectory"`
		ProductTUIConsoleEnabled  bool   `json:"productTUIConsoleEnabled"`
		ProductTUIConsoleColumns  int    `json:"productTUIConsoleColumns"`
		ProductTUIConsoleRows     int    `json:"productTUIConsoleRows"`
		ProductTUIConsoleFontFace string `json:"productTUIConsoleFontFace"`
		ProductTUIConsoleFontSize int    `json:"productTUIConsoleFontSize"`
	}
	if err := json.Unmarshal(content, &metadata); err != nil {
		t.Fatal(err)
	}
	want := []string{
		metadata.Version,
		metadata.ProductName, metadata.ProductShortName, metadata.ProductTagline,
		metadata.Description, metadata.ProductAppID, metadata.ProductProtocol,
		metadata.ProductConfigDirectory,
		fmt.Sprint(metadata.ProductTUIConsoleEnabled),
		fmt.Sprint(metadata.ProductTUIConsoleColumns),
		fmt.Sprint(metadata.ProductTUIConsoleRows),
		metadata.ProductTUIConsoleFontFace,
		fmt.Sprint(metadata.ProductTUIConsoleFontSize),
	}
	got := []string{
		Version,
		DefaultTitle, ShortName, Tagline, Description, StableAppID,
		ProtocolScheme, ConfigDirectory,
		DefaultTUIConsoleEnabled, DefaultTUIConsoleColumns, DefaultTUIConsoleRows,
		DefaultTUIConsoleFontFace, DefaultTUIConsoleFontSize,
	}
	for index := range want {
		if want[index] == "" || want[index] != got[index] {
			t.Fatalf("generated identity field %d=%q; package metadata=%q", index, got[index], want[index])
		}
	}
}

func TestWin32ResourcesMatchCanonicalPackageMetadata(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "web", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		ProductName string `json:"productName"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(content, &metadata); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(filepath.Join("..", "..", "winres", "winres.json"))
	if err != nil {
		t.Fatal(err)
	}
	var resources struct {
		Manifest map[string]map[string]struct {
			Description string `json:"description"`
		} `json:"RT_MANIFEST"`
		Version map[string]map[string]struct {
			Info map[string]map[string]string `json:"info"`
		} `json:"RT_VERSION"`
	}
	if err := json.Unmarshal(content, &resources); err != nil {
		t.Fatal(err)
	}
	manifest := resources.Manifest["#1"]["0409"].Description
	info := resources.Version["#1"]["0409"].Info["0409"]
	if manifest != metadata.ProductName+" host utility" ||
		info["FileDescription"] != metadata.ProductName+" Host" ||
		info["ProductName"] != metadata.ProductName ||
		info["FileVersion"] != metadata.Version ||
		info["ProductVersion"] != metadata.Version {
		t.Fatalf("Win32 resources drifted: manifest=%q info=%#v", manifest, info)
	}
}

func TestTitlePrecedence(t *testing.T) {
	if got := Title("Workshop Controller"); got != "Workshop Controller" {
		t.Fatalf("configured title=%q", got)
	}
	if got := Title(""); got != DefaultTitle {
		t.Fatalf("default title=%q", got)
	}
	if got := ResolveTitle("Workshop Controller", "Temporary Lab", "Flag Lab"); got != "Flag Lab" {
		t.Fatalf("flag title=%q", got)
	}
	if got := ResolveTitle("Workshop Controller", "Temporary Lab", ""); got != "Temporary Lab" {
		t.Fatalf("environment title=%q", got)
	}
}
