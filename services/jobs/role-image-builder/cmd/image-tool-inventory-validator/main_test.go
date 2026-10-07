package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestValidateExactManifestAndBinding(t *testing.T) {
	digest := strings.Repeat("a", 64)
	manifest := runtimecontract.ImageToolManifest{Schema: runtimecontract.ImageInventorySchema,
		SpecSHA256: digest, ImmutableBuildSHA256: digest, RuntimeContractSHA256: digest, Platform: "linux/amd64"}
	for _, probe := range runtimecontract.ImageToolProbes() {
		manifest.Tools = append(manifest.Tools, runtimecontract.ImageToolObservation{Name: probe.Name, Status: "MISSING", Required: probe.Required})
	}
	raw, _ := json.Marshal(manifest)
	var output bytes.Buffer
	if err := validate([]string{"capture-manifest"}, bytes.NewReader(raw), &output); err != nil || !bytes.Equal(raw, output.Bytes()) {
		t.Fatal("canonical manifest was not preserved")
	}
	binding := runtimecontract.ImageToolInventory{Schema: runtimecontract.ImageInventoryBindingSchema,
		ImageDigest: "sha256:" + digest, ProvenanceSHA256: digest,
		Platforms: []runtimecontract.ImagePlatformInventory{{PlatformDigest: "sha256:" + digest,
			ManifestSHA256: runtimecontract.ImageInventorySHA256(raw), Manifest: manifest}}}
	bound, _ := json.Marshal(binding)
	output.Reset()
	if err := validate([]string{"inventory"}, bytes.NewReader(bound), &output); err != nil || output.Len() != 0 {
		t.Fatal("binding rejected or disclosed")
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), '\n'), []byte(`{"private":"PRIVATE_SENTINEL"}`), bytes.Repeat([]byte("x"), runtimecontract.MaximumImageInventoryBytes+1)} {
		output.Reset()
		err := validate([]string{"capture-manifest"}, bytes.NewReader(invalid), &output)
		if err != invalidInventory || output.Len() != 0 || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("invalid input accepted or disclosed")
		}
	}
	if validate([]string{"PRIVATE_SENTINEL"}, bytes.NewReader(raw), &output) != invalidInventory {
		t.Fatal("unknown mode accepted")
	}
	if validate([]string{"capture-manifest"}, bytes.NewReader(raw), failingWriter{}) != invalidInventory {
		t.Fatal("output error not closed")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("PRIVATE_SENTINEL") }
