package runtimecontract

import (
	"encoding/json"
	"strings"
	"testing"
)

// nativeCheckpointFixture supplies an exact synthetic native export and workspace generation.
// nativeCheckpointFixture задаёт точный synthetic native export и workspace generation.
func nativeCheckpointFixture() NativeSessionCheckpoint {
	path, _ := NativeSessionExportPath("ses_native", 2)
	return NativeSessionCheckpoint{SessionID: "ses_native", Version: OpenCodeVersion, Generation: 2, RelativePath: path, SHA256: strings.Repeat("a", 64), SizeBytes: 800, WorkspaceRef: "wks_synthetic", WorkspaceGeneration: 3}
}

// TestNativeSessionDigestAndClosedMetadata prevent source changes under the same RuntimeRevision.
// TestNativeSessionDigestAndClosedMetadata запрещает смену source под той же RuntimeRevision.
func TestNativeSessionDigestAndClosedMetadata(t *testing.T) {
	value := nativeCheckpointFixture()
	raw, _ := json.Marshal(value)
	if got, err := DecodeNativeSessionCheckpoint(raw); err != nil || got != value {
		t.Fatal("valid native owner metadata rejected")
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), raw...), []byte(strings.Replace(string(raw), "{", `{"generation":2,`, 1)), []byte(strings.Replace(string(raw), "{", `{"secret":"synthetic",`, 1))} {
		if _, err := DecodeNativeSessionCheckpoint(invalid); err == nil {
			t.Fatal("ambiguous native owner metadata accepted")
		}
	}
	input := modelProviderRunnerFixture()
	input.NativeSession = &value
	source := RuntimeRevisionCredentialSource{SecretName: "synthetic", SecretUID: "synthetic", SecretResourceVersion: "1"}
	before, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	value.SHA256 = strings.Repeat("b", 64)
	after, err := RuntimeRevisionDigest(input, source)
	if err != nil || before == after {
		t.Fatal("native recovery bytes are not pinned by runtime revision")
	}
}

// TestNativeCheckpointRejectsSubstitution checks every recovery commitment instead of silently cold-starting.
// TestNativeCheckpointRejectsSubstitution проверяет каждый recovery commitment вместо неявного cold start.
func TestNativeCheckpointRejectsSubstitution(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*NativeSessionCheckpoint)
	}{
		{"session", func(value *NativeSessionCheckpoint) { value.SessionID = "ses_foreign" }},
		{"version", func(value *NativeSessionCheckpoint) { value.Version = "unknown" }},
		{"generation", func(value *NativeSessionCheckpoint) { value.Generation++ }},
		{"path", func(value *NativeSessionCheckpoint) { value.RelativePath = "../foreign" }},
		{"digest", func(value *NativeSessionCheckpoint) { value.SHA256 = "missing" }},
		{"quota", func(value *NativeSessionCheckpoint) { value.SizeBytes = MaximumSessionSourceBytes + 1 }},
		{"workspace", func(value *NativeSessionCheckpoint) { value.WorkspaceRef = "" }},
		{"workspace-generation", func(value *NativeSessionCheckpoint) { value.WorkspaceGeneration = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := nativeCheckpointFixture()
			test.change(&value)
			if value.Validate() == nil {
				t.Fatal("invalid native binding accepted")
			}
		})
	}
}

// TestNativeCheckpointRequiresConsistentFileRecovery separates retained generation and S3 restore paths.
// TestNativeCheckpointRequiresConsistentFileRecovery разделяет retained generation и S3 restore paths.
func TestNativeCheckpointRequiresConsistentFileRecovery(t *testing.T) {
	value := nativeCheckpointFixture()
	if value.Validate() != nil {
		t.Fatal("valid checkpoint rejected")
	}
	live := &WorkspaceStorageBinding{WorkspaceRef: value.WorkspaceRef, Generation: 3}
	if value.ValidateWorkspace(live) != nil {
		t.Fatal("matching retained tree rejected")
	}
	live.Generation = 4
	if value.ValidateWorkspace(live) == nil {
		t.Fatal("history joined with a different retained tree")
	}
	live.Source = &WorkspaceCheckpointBinding{WorkspaceRef: value.WorkspaceRef, Generation: 3}
	if value.ValidateWorkspace(live) != nil {
		t.Fatal("matching source restore rejected")
	}
	live.Source.Generation = 2
	if value.ValidateWorkspace(live) == nil {
		t.Fatal("history joined with older restored files")
	}
	if value.ValidateWorkspace(nil) == nil {
		t.Fatal("workspace checkpoint silently discarded")
	}
	for _, id := range []string{"../native", "ses_native/path", "ses_native?x=1", "", "ses_native\x00"} {
		if _, err := NativeSessionExportPath(id, 1); err == nil {
			t.Fatal("non-native path accepted")
		}
	}
}
