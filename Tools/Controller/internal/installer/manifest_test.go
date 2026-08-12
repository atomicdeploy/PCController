package installer

import (
	"encoding/binary"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/productidentity"
)

func TestVersionResourceIdentityRequiresExactStructuredFields(t *testing.T) {
	const version = "1.2.3"
	const built = "2026-08-12T13:55:51Z"
	source := strings.Repeat("a", 64)
	manifest := PackageManifest{SourceSHA256: source, BuildTime: built}
	host := hostPackageManifest{}
	host.Identity.Version = version

	resource, err := peVersionResource(minimalResourcePE(version, source, built, "overlay decoy "+strings.Repeat("b", 64)))
	if err != nil {
		t.Fatalf("extract RT_VERSION: %v", err)
	}
	values, err := parseVersionInfoStrings(resource)
	if err != nil {
		t.Fatalf("parse RT_VERSION: %v", err)
	}
	if err := verifyWindowsResourceIdentity(values, manifest, host); err != nil {
		t.Fatalf("verify structured version identity: %v", err)
	}

	values["PrivateBuild"] = strings.Repeat("b", 64)
	if err := verifyWindowsResourceIdentity(values, manifest, host); err == nil || !strings.Contains(err.Error(), "source hash") {
		t.Fatalf("tampered source hash error = %v, want declared source-hash failure", err)
	}
}

func TestVersionResourceParserRejectsDuplicateVersionPayload(t *testing.T) {
	content := minimalResourcePE("1.2.3", strings.Repeat("a", 64), "2026-08-12T13:55:51Z", "")
	location, err := parsePEResourceLocation(content)
	if err != nil {
		t.Fatal(err)
	}
	resource := content[location.rawOffset : location.rawOffset+location.size]
	// Add a second language entry and point it at the same valid data payload:
	// exact duplication is rejected rather than silently choosing one.
	binary.LittleEndian.PutUint16(resource[0x30+14:0x30+16], 2)
	if _, err := peVersionResource(content); err == nil || !strings.Contains(err.Error(), "exactly one RT_VERSION payload") {
		t.Fatalf("duplicate payload error = %v", err)
	}
}

func TestVersionResourceParserRejectsMalformedBoundsAndDecoys(t *testing.T) {
	content := minimalResourcePE("1.2.3", strings.Repeat("a", 64), "2026-08-12T13:55:51Z", productidentity.DefaultTitle)
	location, err := parsePEResourceLocation(content)
	if err != nil {
		t.Fatal(err)
	}
	resource := content[location.rawOffset : location.rawOffset+location.size]
	// The overlay carries valid-looking text but cannot satisfy a broken data RVA.
	binary.LittleEndian.PutUint32(resource[0x48:0x4c], 0x1000+uint32(location.size)+4)
	if _, err := peVersionResource(content); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("out-of-bounds resource error = %v", err)
	}
}
