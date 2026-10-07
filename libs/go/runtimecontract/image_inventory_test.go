package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func inventoryFixture() ImageToolInventory {
	digest := strings.Repeat("a", 64)
	manifest := ImageToolManifest{Schema: ImageInventorySchema, SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64"}
	for _, probe := range ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	raw, _ := json.Marshal(manifest)
	return ImageToolInventory{Schema: ImageInventoryBindingSchema, ImageDigest: "sha256:" + digest, ProvenanceSHA256: digest, Platforms: []ImagePlatformInventory{{PlatformDigest: "sha256:" + digest, ManifestSHA256: ImageInventorySHA256(raw), Manifest: manifest}}}
}

func TestGoimportsProbeUsesSuccessfulFormattingReadinessNotUsage(t *testing.T) {
	for _, probe := range ImageToolProbes() {
		if probe.Name != "goimports" {
			continue
		}
		if len(probe.Args) != 0 || !probe.Required {
			t.Fatal("goimports must format stdin EOF, not accept a failed usage probe")
		}
		return
	}
	t.Fatal("required goimports probe is absent")
}

func TestChromiumProbeObservesNativeExecutableBeforeDistributionWrapper(t *testing.T) {
	for _, probe := range ImageToolProbes() {
		if probe.Name == "chromium" {
			if len(probe.Paths) == 0 || probe.Paths[0] != "/usr/lib/chromium/chromium" || len(probe.Args) != 1 || probe.Args[0] != "--version" || !probe.Required {
				t.Fatal("native Chromium version probe is missing")
			}
			return
		}
	}
	t.Fatal("required Chromium probe is absent")
}

func TestImageToolInventoryClosedSchemaAndBinding(t *testing.T) {
	value := inventoryFixture()
	raw, _ := json.Marshal(value)
	if _, err := DecodeImageToolInventory(raw); err != nil {
		t.Fatal("valid observed missing inventory rejected")
	}
	count := 0
	for _, probe := range ImageToolProbes() {
		if probe.Required {
			count++
		}
	}
	if count != 38 || len(ImageToolProbes()) != 50 {
		t.Fatal("required QA registry is incomplete")
	}
	for _, test := range []struct {
		name   string
		mutate func(*ImageToolInventory)
	}{
		{"digest", func(v *ImageToolInventory) { v.Platforms[0].ManifestSHA256 = strings.Repeat("b", 64) }},
		{"unknown tool", func(v *ImageToolInventory) { v.Platforms[0].Manifest.Tools[0].Name = "arbitrary" }},
		{"missing fabricated", func(v *ImageToolInventory) { v.Platforms[0].Manifest.Tools[0].Version = "1.2.3" }},
		{"not verified", func(v *ImageToolInventory) { v.Platforms[0].Manifest.Tools[0].Status = "AVAILABLE" }},
		{"duplicate", func(v *ImageToolInventory) { v.Platforms = append(v.Platforms, v.Platforms[0]) }},
		{"required forged", func(v *ImageToolInventory) { v.Platforms[0].Manifest.Tools[0].Required = false }},
		{"unsafe version", func(v *ImageToolInventory) {
			v.Platforms[0].Manifest.Tools[0] = ImageToolObservation{Name: "bash", Status: "VERIFIED", Path: "/bin/bash", Version: "PRIVATE_SENTINEL", SHA256: strings.Repeat("a", 64), Required: true}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := inventoryFixture()
			test.mutate(&v)
			data, _ := json.Marshal(v)
			if _, err := DecodeImageToolInventory(data); err == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
	for _, data := range [][]byte{
		[]byte(strings.Replace(string(raw), `"schema":`, `"schema":"duplicate","schema":`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":`, `"private_payload":"PRIVATE_SENTINEL","schema":`, 1)),
		append(raw, []byte(`{}`)...), make([]byte, MaximumImageInventoryBytes+1),
	} {
		if _, err := DecodeImageToolInventory(data); err == nil || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid JSON accepted or disclosed")
		}
	}
}

func TestImageToolManifestExactCanonicalBytes(t *testing.T) {
	manifest := inventoryFixture().Platforms[0].Manifest
	raw, _ := json.Marshal(manifest)
	if _, err := DecodeImageToolManifest(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeImageToolManifest(append(raw, '\n')); err == nil {
		t.Fatal("noncanonical manifest accepted")
	}
}
