package runtimecontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// workspaceArchiveFixture creates closed synthetic metadata without source bytes or credentials.
// workspaceArchiveFixture создаёт closed synthetic metadata без source bytes или credentials.
func workspaceArchiveFixture() WorkspaceArchiveManifest {
	return WorkspaceArchiveManifest{
		Schema:       WorkspaceArchiveSchema,
		Scope:        WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
		WorkspaceRef: "wks_fixture000", Generation: 1, CheckpointRef: "wcp_fixture000",
		Writer: WorkspaceWriterCommitment{RunRef: "run_fixture000", NodeRef: "nod_fixture000", TurnRef: "trn_fixture000", RuntimeRevisionRef: "rrv_fixture000",
			RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64),
			LeaseRef: "lea_fixture000", LeaseGeneration: 1, Attempt: 1, PodUID: "pod-fixture-uid", PVCUID: "pvc-fixture-uid", JoinSHA256: strings.Repeat("d", 64)},
		TreeSHA256: strings.Repeat("e", 64), StreamBytes: 1024,
		Parts: []WorkspaceArchivePart{{Index: 1, Version: "version-one", ETag: "etag-one", Digest: "sha256:" + strings.Repeat("f", 64), SizeBytes: 1024}},
	}
}

// TestWorkspaceManifestRejectsUnboundOrLossyWire checks the real codec and preserves its input.
// TestWorkspaceManifestRejectsUnboundOrLossyWire проверяет настоящий codec без изменения input.
func TestWorkspaceManifestRejectsUnboundOrLossyWire(t *testing.T) {
	value := workspaceArchiveFixture()
	raw, err := EncodeWorkspaceArchiveManifest(value)
	if err != nil {
		t.Fatal("valid synthetic checkpoint rejected")
	}
	decoded, err := DecodeWorkspaceArchiveManifest(raw)
	if err != nil || decoded.Writer != value.Writer || decoded.Scope != value.Scope || len(decoded.Parts) != 1 {
		t.Fatal("checkpoint lost immutable owner commitments")
	}
	var reordered map[string]any
	if json.Unmarshal(raw, &reordered) != nil {
		t.Fatal("valid wire cannot be decoded for the order regression")
	}
	unordered, err := json.Marshal(reordered)
	if err != nil {
		t.Fatal("valid wire cannot be reordered")
	}
	for name, invalid := range map[string][]byte{
		"duplicate": bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"unknown","schema":`), 1),
		"unknown":   append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"private_auth":"forbidden"}`)...),
		"trailing":  append(append([]byte{}, raw...), []byte(`{}`)...),
		"oversize":  bytes.Repeat([]byte(" "), MaximumWorkspaceManifestBytes+1),
		"unordered": unordered,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeWorkspaceArchiveManifest(invalid); err == nil {
				t.Fatal("malformed checkpoint metadata accepted")
			}
		})
	}
	if strings.Contains(string(raw), "lease_fence") || strings.Contains(string(raw), "private_auth") {
		t.Fatal("private credential field entered the checkpoint contract")
	}
}

// TestWorkspaceManifestBoundsPartsAndExistingBudgets proves metadata size and closed boundaries.
// It does not allocate the declared GiB stream or claim filesystem checkpoint acceptance.
// TestWorkspaceManifestBoundsPartsAndExistingBudgets доказывает metadata size и closed boundaries.
// Он не выделяет заявленный GiB stream и не утверждает filesystem checkpoint acceptance.
func TestWorkspaceManifestBoundsPartsAndExistingBudgets(t *testing.T) {
	value := workspaceArchiveFixture()
	value.Bytes, value.Entries = RuntimeWorkspaceWritableBytes, RuntimeWorkspaceMaximumFiles
	maximum, err := MaximumWorkspaceStreamBytes(value.Bytes, value.Entries)
	if err != nil || maximum > int64(MaximumWorkspaceArchiveParts)*MaximumWorkspaceArchivePartBytes {
		t.Fatal("existing work quotas exceed the bounded object count")
	}
	value.StreamBytes = maximum
	value.Parts = nil
	remaining := maximum
	for remaining > 0 {
		size := min(remaining, MaximumWorkspaceArchivePartBytes)
		value.Parts = append(value.Parts, WorkspaceArchivePart{Index: int32(len(value.Parts) + 1), Version: strings.Repeat("v", 1024),
			ETag: strings.Repeat("e", 256), Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: size})
		remaining -= size
	}
	raw, err := EncodeWorkspaceArchiveManifest(value)
	if err != nil || len(raw) > MaximumWorkspaceManifestBytes {
		t.Fatal("largest derived data stream exceeds the original manifest budget")
	}
	escaped := value
	escaped.Parts = append([]WorkspaceArchivePart(nil), value.Parts...)
	for index := range escaped.Parts {
		escaped.Parts[index].Version = strings.Repeat(`"`, 1024)
		escaped.Parts[index].ETag = strings.Repeat(`\`, 256)
	}
	if escaped.Validate() != nil {
		t.Fatal("escaping fixture changed the independently bounded metadata shape")
	}
	if _, err := EncodeWorkspaceArchiveManifest(escaped); err == nil {
		t.Fatal("JSON escaping bypassed the encoded 64KiB ceiling")
	}
	metadata := append([]WorkspaceArchivePart(nil), value.Parts...)
	for len(metadata) < MaximumWorkspaceArchiveParts {
		part := metadata[0]
		part.Index = int32(len(metadata) + 1)
		metadata = append(metadata, part)
	}
	value.Parts = metadata
	worstWire, err := json.Marshal(value)
	if err != nil || len(worstWire) > MaximumWorkspaceManifestBytes {
		t.Fatal("32 maximum metadata commitments exceed the unchanged wire ceiling")
	}
	for _, change := range []func(*WorkspaceArchiveManifest){
		func(m *WorkspaceArchiveManifest) {
			part := m.Parts[0]
			m.Parts = nil
			for index := 1; index <= MaximumWorkspaceArchiveParts+1; index++ {
				part.Index = int32(index)
				part.SizeBytes = MaximumWorkspaceArchivePartBytes
				m.Parts = append(m.Parts, part)
			}
			m.StreamBytes = int64(len(m.Parts)) * MaximumWorkspaceArchivePartBytes
		},
		func(m *WorkspaceArchiveManifest) { m.Parts[0].SizeBytes = MaximumWorkspaceArchivePartBytes + 1 },
		func(m *WorkspaceArchiveManifest) { m.Parts[0].Version = "" },
		func(m *WorkspaceArchiveManifest) { m.Parts[0].ETag = strings.Repeat("e", 257) },
		func(m *WorkspaceArchiveManifest) { m.Bytes = RuntimeWorkspaceWritableBytes + 1 },
		func(m *WorkspaceArchiveManifest) { m.Entries = RuntimeWorkspaceMaximumFiles + 1 },
		func(m *WorkspaceArchiveManifest) { m.Writer.JoinSHA256 = "unknown" },
	} {
		invalid := workspaceArchiveFixture()
		change(&invalid)
		if _, err := EncodeWorkspaceArchiveManifest(invalid); err == nil {
			t.Fatal("changed part, owner proof or existing quota was accepted")
		}
	}
}

// TestWorkspaceGenerationNeverSelectsAnAlternateParent validates exact mounts and source scope.
// Source shape is not owner authority; authoritative retry selection remains a repository test.
// TestWorkspaceGenerationNeverSelectsAnAlternateParent проверяет exact mounts и source scope.
// Source shape не является owner authority; authoritative retry selection проверяется repository.
func TestWorkspaceGenerationNeverSelectsAnAlternateParent(t *testing.T) {
	manifest := workspaceArchiveFixture()
	input := RunnerInput{Mode: RunnerModeTurn, AssistantScope: AssistantScopeNone, OrganizationRef: manifest.Scope.OrganizationRef, ProjectRef: manifest.Scope.ProjectRef,
		SessionRef: manifest.Scope.SessionRef, AgentRef: manifest.Scope.AgentRef, WorkspacePolicy: RuntimeWorkspacePolicyV1()}
	value := WorkspaceStorageBinding{Profile: WorkspaceStorageProfile, Scope: manifest.Scope, WorkspaceRef: manifest.WorkspaceRef, Generation: 2}
	path, err := value.GenerationSubPath()
	if err != nil || path != "workspaces/wks_fixture000/g2/tree" || value.ValidateForInput(input) != nil {
		t.Fatal("canonical isolated generation rejected")
	}
	for name, change := range map[string]func(*WorkspaceStorageBinding){
		"traversal": func(v *WorkspaceStorageBinding) { v.WorkspaceRef = "wks_fixture000/../../other" },
		"parent":    func(v *WorkspaceStorageBinding) { v.WorkspaceRef = ".." },
		"zero":      func(v *WorkspaceStorageBinding) { v.Generation = 0 },
		"profile":   func(v *WorkspaceStorageBinding) { v.Profile = "UNSUPPORTED" },
		"session":   func(v *WorkspaceStorageBinding) { v.Scope.SessionRef = "ses_foreign000" },
		"project":   func(v *WorkspaceStorageBinding) { v.Scope.ProjectRef = "prj_foreign000" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := value
			change(&invalid)
			if invalid.ValidateForInput(input) == nil {
				t.Fatal("alternate workspace identity became valid")
			}
		})
	}
	checkpoint := WorkspaceCheckpointBinding{Scope: manifest.Scope, WorkspaceRef: manifest.WorkspaceRef, Generation: 1, CheckpointRef: manifest.CheckpointRef,
		TreeSHA256: manifest.TreeSHA256, Manifest: WorkspaceObjectCommitment{Version: "one", ETag: "one", Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1024}}
	checkpoint.Manifest.Key = checkpoint.ObjectPrefix() + "manifest.json"
	value.Source = &checkpoint
	if value.ValidateForInput(input) != nil {
		t.Fatal("exact same-owner checkpoint metadata rejected")
	}
	checkpoint.Scope.ProjectRef = "prj_foreign000"
	checkpoint.Manifest.Key = checkpoint.ObjectPrefix() + "manifest.json"
	if value.ValidateForInput(input) == nil {
		t.Fatal("foreign project checkpoint metadata accepted")
	}
}

// TestWorkspaceStorageIsOptionalAndDigestBound preserves legacy omission and binds fresh work inputs.
// It never treats a matching digest as permission to restore or acquire a writer.
// TestWorkspaceStorageIsOptionalAndDigestBound сохраняет legacy omission и связывает fresh work inputs.
// Совпадающий digest не считается правом restore или получения writer.
func TestWorkspaceStorageIsOptionalAndDigestBound(t *testing.T) {
	_, input, binding := workspaceQuiescenceFixture()
	input.WorkloadInstance = "runtime-controller-fixture"
	before, err := json.Marshal(input)
	if err != nil || bytes.Contains(before, []byte(`"workspace_storage"`)) {
		t.Fatal("absent legacy storage policy materialized on the wire")
	}
	credential := RuntimeRevisionCredentialSource{SecretName: "synthetic-provider", SecretUID: "synthetic-uid", SecretResourceVersion: "1"}
	legacyDigest, err := RuntimeRevisionDigest(input, credential)
	if err != nil {
		t.Fatal("synthetic revision cannot be hashed")
	}
	input.WorkspaceStorage = &binding
	durableDigest, err := RuntimeRevisionDigest(input, credential)
	if err != nil || durableDigest == legacyDigest {
		t.Fatal("owner-selected work generation disappeared from the revision digest")
	}
	binding.Generation++
	freshDigest, err := RuntimeRevisionDigest(input, credential)
	if err != nil || freshDigest == durableDigest {
		t.Fatal("fresh generation reused the previous revision digest")
	}
	for _, scope := range []AssistantScope{AssistantScopeSystem, AssistantScopeProject} {
		input.AssistantScope = scope
		if binding.ValidateForInput(input) == nil {
			t.Fatal("employee persistence policy widened into an assistant profile")
		}
	}
}
