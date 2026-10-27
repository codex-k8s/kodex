package runtimecontract

import (
	"bytes"
	"encoding/json"
	"testing"
)

// workspaceVolumeWorkFixture supplies distinct owner task identities and a positive physical pin without an execution grant.
// workspaceVolumeWorkFixture задаёт разные owner task identities и positive physical pin без execution grant.
func workspaceVolumeWorkFixture(kind string, attempt int32) WorkspaceVolumeWork {
	preparation := workspaceVolumePreparationFixture()
	input := WorkspaceVolumeInput{Profile: WorkspaceVolumeLocalPathProfile, Scope: preparation.Scope,
		WorkspaceRef: preparation.WorkspaceRef, VolumeGeneration: preparation.VolumeGeneration, CreateTaskRef: "svt_create0000"}
	ref := input.CreateTaskRef
	if kind == WorkspaceVolumePrepare {
		input.PreparationTaskRef, input.PVCUID = preparation.TaskRef, preparation.PVCUID
		ref = preparation.TaskRef
	}
	value := WorkspaceVolumeWork{TaskRef: ref, Kind: kind, Attempt: attempt, ReconcileOnly: attempt > 1, Volume: input}
	value.InputDigest, _ = value.Digest()
	return value
}

// TestWorkspaceVolumeAttemptContract closes the effective operation across every bounded retry without assigning write credit to reconciliation.
// TestWorkspaceVolumeAttemptContract замыкает effective operation при каждом bounded retry без write credit для reconciliation.
func TestWorkspaceVolumeAttemptContract(t *testing.T) {
	for _, kind := range []string{WorkspaceVolumeCreate, WorkspaceVolumePrepare} {
		seen := make(map[string]bool)
		for attempt := int32(1); attempt <= 5; attempt++ {
			value := workspaceVolumeWorkFixture(kind, attempt)
			raw, err := EncodeWorkspaceVolumeWork(value)
			decoded, decodeErr := DecodeWorkspaceVolumeWork(raw)
			if err != nil || decodeErr != nil || decoded != value || seen[value.InputDigest] {
				t.Fatal("effective attempt lost its exact operation or canonical digest")
			}
			seen[value.InputDigest] = true
			if value.ReconcileOnly != (attempt > 1) {
				t.Fatal("retry acquired physical write credit")
			}
			preparation, err := value.Preparation()
			if kind == WorkspaceVolumePrepare {
				if err != nil || preparation.TaskRef != value.Volume.PreparationTaskRef || preparation.PVCUID != value.Volume.PVCUID {
					t.Fatal("preparation changed its logical identity or physical pin")
				}
			} else if err == nil || preparation != (WorkspaceVolumePreparation{}) {
				t.Fatal("create impersonated a preparation task")
			}
		}
	}
	original := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
	recovery := original
	recovery.TaskRef, recovery.Volume.ReadOnly, recovery.ReconcileOnly = "svt_recovery00", true, true
	recovery.InputDigest, _ = recovery.Digest()
	if recovery.Validate() != nil || recovery.InputDigest == original.InputDigest {
		t.Fatal("separate read-only recovery failed to bind its distinct task")
	}
	before, _ := original.Preparation()
	after, _ := recovery.Preparation()
	if after != before {
		t.Fatal("recovery rewrote the original sealed preparation identity")
	}
}

// TestWorkspaceVolumeAttemptRejectsMutations denies changed scope, operation or physical custody both with stale digests and invalid recomputed shapes.
// TestWorkspaceVolumeAttemptRejectsMutations отклоняет changed scope/operation/custody со stale digest и invalid recomputed shape.
func TestWorkspaceVolumeAttemptRejectsMutations(t *testing.T) {
	for name, mutate := range map[string]func(*WorkspaceVolumeWork){
		"unknown-kind":        func(v *WorkspaceVolumeWork) { v.Kind = "RESTORE" },
		"legacy-ref":          func(v *WorkspaceVolumeWork) { v.TaskRef = "sat_fixture000" },
		"zero-attempt":        func(v *WorkspaceVolumeWork) { v.Attempt = 0 },
		"sixth-attempt":       func(v *WorkspaceVolumeWork) { v.Attempt = 6; v.ReconcileOnly = true },
		"project":             func(v *WorkspaceVolumeWork) { v.Volume.Scope.ProjectRef = "prj_foreign000" },
		"work":                func(v *WorkspaceVolumeWork) { v.Volume.WorkspaceRef = "wks_foreign000" },
		"volume-generation":   func(v *WorkspaceVolumeWork) { v.Volume.VolumeGeneration++ },
		"physical-uid":        func(v *WorkspaceVolumeWork) { v.Volume.PVCUID = "20000000-0000-4000-8000-000000000001" },
		"profile":             func(v *WorkspaceVolumeWork) { v.Volume.Profile = "OTHER" },
		"missing-uid":         func(v *WorkspaceVolumeWork) { v.Volume.PVCUID = "" },
		"missing-create":      func(v *WorkspaceVolumeWork) { v.Volume.CreateTaskRef = "" },
		"missing-preparation": func(v *WorkspaceVolumeWork) { v.Volume.PreparationTaskRef = "" },
		"borrowed-write":      func(v *WorkspaceVolumeWork) { v.TaskRef = "svt_recovery00" },
		"same-create-preparation": func(v *WorkspaceVolumeWork) {
			v.TaskRef, v.Volume.PreparationTaskRef, v.Volume.ReadOnly, v.ReconcileOnly = v.Volume.CreateTaskRef, v.Volume.CreateTaskRef, true, true
		},
		"read-only-write-credit": func(v *WorkspaceVolumeWork) { v.Volume.ReadOnly = true },
	} {
		t.Run(name, func(t *testing.T) {
			value := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
			mutate(&value)
			if value.Validate() == nil {
				t.Fatal("mutated effective input retained an old digest")
			}
			if _, err := EncodeWorkspaceVolumeWork(value); err == nil {
				t.Fatal("mutated effective input was serialized")
			}
		})
	}
	for _, kind := range []string{WorkspaceVolumeCreate, WorkspaceVolumePrepare} {
		retry := workspaceVolumeWorkFixture(kind, 2)
		retry.ReconcileOnly = false
		if _, err := retry.Digest(); err == nil {
			t.Fatal("recomputed retry digest admitted physical write credit")
		}
	}
	create := workspaceVolumeWorkFixture(WorkspaceVolumeCreate, 1)
	create.Volume.PVCUID = "10000000-0000-4000-8000-000000000001"
	if _, err := create.Digest(); err == nil {
		t.Fatal("create admitted an already named physical volume")
	}
}

// TestWorkspaceVolumeWorkCodecRejectsUntrustedJSON prevents a consumer from interpreting bytes differently from the owner's immutable snapshot.
// TestWorkspaceVolumeWorkCodecRejectsUntrustedJSON не позволяет consumer трактовать bytes иначе, чем immutable snapshot владельца.
func TestWorkspaceVolumeWorkCodecRejectsUntrustedJSON(t *testing.T) {
	value := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
	raw, err := EncodeWorkspaceVolumeWork(value)
	if err != nil {
		t.Fatal(err)
	}
	var expanded bytes.Buffer
	if json.Indent(&expanded, raw, "", " ") != nil {
		t.Fatal("fixture JSON invalid")
	}
	for name, bad := range map[string][]byte{
		"empty": nil, "whitespace": append(append([]byte(nil), raw...), '\n'), "reformatted": expanded.Bytes(),
		"trailing":  append(append([]byte(nil), raw...), []byte("{}")...),
		"unknown":   append([]byte(`{"unknown":true,`), raw[1:]...),
		"duplicate": bytes.Replace(raw, []byte(`"attempt":1`), []byte(`"attempt":1,"attempt":1`), 1),
		"alias":     bytes.Replace(raw, []byte(`"volume"`), []byte(`"Volume"`), 1),
		"oversize":  bytes.Repeat([]byte(" "), MaximumWorkspaceVolumeReadyBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := DecodeWorkspaceVolumeWork(bad)
			if err == nil || decoded != (WorkspaceVolumeWork{}) {
				t.Fatal("untrusted effective input reached consumer dispatch")
			}
		})
	}
}
