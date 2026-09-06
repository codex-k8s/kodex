package runtimecontract

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestRuntimeFileCatalogBindsSnapshotAndPreservesLegacyShape(t *testing.T) {
	input := validRunnerInputFixture()
	source := RuntimeRevisionCredentialSource{SecretName: "fixture", SecretUID: "fixture-uid", SecretResourceVersion: "1"}
	legacy, err := json.Marshal(input)
	if err != nil || strings.Contains(string(legacy), "file_catalog") {
		t.Fatal("absent catalog changed legacy runtime shape")
	}
	input.FileCatalog = &RuntimeFileCatalog{Ref: "vfc_fixture1", Digest: strings.Repeat("a", 64), Total: 2, Purposes: []string{FilePurposeProject, FilePurposeRunResult}}
	if err := input.FileCatalog.Validate(); err != nil {
		t.Fatal(err)
	}
	original, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RuntimeFileCatalog){
		func(c *RuntimeFileCatalog) { c.Ref = "vfc_fixture2" },
		func(c *RuntimeFileCatalog) { c.Digest = strings.Repeat("b", 64) },
		func(c *RuntimeFileCatalog) { c.Total++ },
		func(c *RuntimeFileCatalog) { c.Purposes = []string{FilePurposeWorkspaceInput} },
	} {
		changed := input
		catalog := *input.FileCatalog
		catalog.Purposes = slices.Clone(catalog.Purposes)
		mutate(&catalog)
		changed.FileCatalog = &catalog
		if digest, err := RuntimeRevisionDigest(changed, source); err != nil || digest == original {
			t.Fatal("changed catalog did not invalidate runtime digest")
		}
	}
	for _, purposes := range [][]string{nil, {"ALL"}, {FilePurposeProject, FilePurposeProject}, {FilePurposeWorkspaceInput, FilePurposeProject}} {
		invalid := *input.FileCatalog
		invalid.Purposes = purposes
		if invalid.Validate() == nil {
			t.Fatal("invalid file scope was accepted")
		}
	}
	invalid := *input.FileCatalog
	invalid.Ref = "rrev_fixture1"
	if invalid.Validate() == nil {
		t.Fatal("wrong catalog reference kind accepted")
	}
}
