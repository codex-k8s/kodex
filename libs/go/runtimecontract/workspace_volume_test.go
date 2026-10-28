package runtimecontract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// workspaceVolumePreparationFixture supplies bounded local metadata without a model execution, provider credential or storage grant.
// workspaceVolumePreparationFixture задаёт bounded local metadata без model execution, provider credential или storage grant.
func workspaceVolumePreparationFixture() WorkspaceVolumePreparation {
	return WorkspaceVolumePreparation{Scope: WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000",
		SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}, WorkspaceRef: "wks_fixture000", TaskRef: "svt_fixture000",
		PVCUID: "10000000-0000-4000-8000-000000000001", VolumeGeneration: 1}
}

// TestWorkspaceVolumeMetadataRoundTrip preserves logical preparation and physical inode pins without treating either as a writer grant.
// TestWorkspaceVolumeMetadataRoundTrip сохраняет logical preparation/physical inode pins, не объявляя их writer grant.
func TestWorkspaceVolumeMetadataRoundTrip(t *testing.T) {
	preparation := workspaceVolumePreparationFixture()
	raw, err := EncodeWorkspaceVolumePreparation(preparation)
	decoded, decodeErr := DecodeWorkspaceVolumePreparation(raw)
	if err != nil || decodeErr != nil || decoded != preparation || len(raw) > MaximumWorkspaceVolumeReadyBytes {
		t.Fatal("bounded preparation roundtrip failed")
	}
	ready := WorkspaceVolumeReady{Schema: WorkspaceVolumeReadySchema, Preparation: preparation, Device: 1, Inode: 2}
	sealed, err := EncodeWorkspaceVolumeReady(ready)
	observed, decodeErr := DecodeWorkspaceVolumeReady(sealed)
	if err != nil || decodeErr != nil || observed != ready || len(sealed) > MaximumWorkspaceVolumeReadyBytes {
		t.Fatal("bounded ready observation roundtrip failed")
	}
	for _, forbidden := range []string{"fence", "runtime_revision", "provider", "password", "token"} {
		if bytes.Contains(sealed, []byte(forbidden)) {
			t.Fatal("local root record acquired unrelated authority or credential fields")
		}
	}
}

// TestWorkspaceVolumePreparationRejectsMissingAndPathLikeIdentity closes metadata parsing before any filesystem initialization.
// TestWorkspaceVolumePreparationRejectsMissingAndPathLikeIdentity закрывает metadata parsing до filesystem initialization.
func TestWorkspaceVolumePreparationRejectsMissingAndPathLikeIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*WorkspaceVolumePreparation){
		"scope":      func(v *WorkspaceVolumePreparation) { v.Scope.ProjectRef = "" },
		"work":       func(v *WorkspaceVolumePreparation) { v.WorkspaceRef = "wks_fixture000/../other" },
		"task":       func(v *WorkspaceVolumePreparation) { v.TaskRef = "svt_fixture000/other" },
		"task-kind":  func(v *WorkspaceVolumePreparation) { v.TaskRef = "lea_fixture000" },
		"pvc":        func(v *WorkspaceVolumePreparation) { v.PVCUID = "" },
		"pvc-path":   func(v *WorkspaceVolumePreparation) { v.PVCUID = "../other" },
		"pvc-upper":  func(v *WorkspaceVolumePreparation) { v.PVCUID = "PVC" },
		"pvc-bound":  func(v *WorkspaceVolumePreparation) { v.PVCUID = strings.Repeat("a", 129) },
		"generation": func(v *WorkspaceVolumePreparation) { v.VolumeGeneration = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			value := workspaceVolumePreparationFixture()
			mutate(&value)
			if value.Validate() == nil {
				t.Fatal("invalid volume identity accepted")
			}
			if _, err := EncodeWorkspaceVolumePreparation(value); err == nil {
				t.Fatal("invalid preparation encoded")
			}
			if _, err := EncodeWorkspaceVolumeReady(WorkspaceVolumeReady{Schema: WorkspaceVolumeReadySchema, Preparation: value, Device: 1, Inode: 2}); err == nil {
				t.Fatal("invalid ready record encoded")
			}
		})
	}
}

// TestWorkspaceVolumeCodecsRejectNoncanonicalInput retains strict field spelling, duplicate rejection and the bounded metadata ceiling.
// TestWorkspaceVolumeCodecsRejectNoncanonicalInput сохраняет strict spelling, duplicate rejection и bounded metadata ceiling.
func TestWorkspaceVolumeCodecsRejectNoncanonicalInput(t *testing.T) {
	preparation := workspaceVolumePreparationFixture()
	scope, err := json.Marshal(preparation.Scope)
	if err != nil {
		t.Fatal(err)
	}
	ready := WorkspaceVolumeReady{Schema: WorkspaceVolumeReadySchema, Preparation: preparation, Device: 1, Inode: 2}
	for _, record := range []bool{false, true} {
		encode := func() []byte {
			if record {
				raw, _ := EncodeWorkspaceVolumeReady(ready)
				return raw
			}
			raw, _ := EncodeWorkspaceVolumePreparation(preparation)
			return raw
		}
		for name, corrupt := range map[string]func([]byte) []byte{
			"empty":      func([]byte) []byte { return nil },
			"whitespace": func(v []byte) []byte { return append(v, '\n') },
			"trailing":   func(v []byte) []byte { return append(v, []byte("{}")...) },
			"oversize":   func([]byte) []byte { return bytes.Repeat([]byte(" "), MaximumWorkspaceVolumeReadyBytes+1) },
			"unknown":    func(v []byte) []byte { return append([]byte(`{"unknown":true,`), v[1:]...) },
			"duplicate": func(v []byte) []byte {
				return bytes.Replace(v, []byte(`"volume_generation":1`), []byte(`"volume_generation":1,"volume_generation":1`), 1)
			},
			"alias": func(v []byte) []byte { return bytes.Replace(v, []byte(`"scope"`), []byte(`"Scope"`), 1) },
			"null-scope": func(v []byte) []byte {
				return bytes.Replace(v, append([]byte(`"scope":`), scope...), []byte(`"scope":null`), 1)
			},
		} {
			t.Run(name+"/"+map[bool]string{true: "ready", false: "preparation"}[record], func(t *testing.T) {
				raw := corrupt(encode())
				if record {
					if observed, err := DecodeWorkspaceVolumeReady(raw); err == nil || observed != (WorkspaceVolumeReady{}) {
						t.Fatal("noncanonical ready metadata accepted")
					}
				} else if observed, err := DecodeWorkspaceVolumePreparation(raw); err == nil || observed != (WorkspaceVolumePreparation{}) {
					t.Fatal("noncanonical preparation metadata accepted")
				}
			})
		}
	}
	for _, mutate := range []func(*WorkspaceVolumeReady){
		func(v *WorkspaceVolumeReady) { v.Device = 0 }, func(v *WorkspaceVolumeReady) { v.Inode = 0 }, func(v *WorkspaceVolumeReady) { v.Schema = "unknown" },
	} {
		invalid := ready
		mutate(&invalid)
		if _, err := EncodeWorkspaceVolumeReady(invalid); err == nil {
			t.Fatal("missing inode or unknown schema accepted")
		}
	}
}
