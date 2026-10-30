package runtimecontract

import "testing"

// workspaceVolumeObservationFixture supplies the exact canonical observation of a preparation without claiming a real physical join.
// workspaceVolumeObservationFixture задаёт exact canonical observation подготовки без заявления о настоящем physical join.
func workspaceVolumeObservationFixture(work WorkspaceVolumeWork) WorkspaceVolumeReady {
	preparation, _ := work.Preparation()
	return WorkspaceVolumeReady{Schema: WorkspaceVolumeReadySchema, Preparation: preparation, Device: 1, Inode: 2}
}

// TestWorkspaceVolumeObservationPreservesOperationBoundary accepts a first positive Create and exact preparation facts, including read-only rejoin.
// Reconciliation of an unknown Create never becomes a new positive Create response.
// TestWorkspaceVolumeObservationPreservesOperationBoundary принимает первый positive Create и exact preparation facts, включая read-only rejoin.
// Reconciliation неизвестного Create не превращается в новый positive Create response.
func TestWorkspaceVolumeObservationPreservesOperationBoundary(t *testing.T) {
	const pvcUID = "10000000-0000-4000-8000-000000000001"
	const podUID = "20000000-0000-4000-8000-000000000001"
	const jobUID = "30000000-0000-4000-8000-000000000001"
	create := workspaceVolumeWorkFixture(WorkspaceVolumeCreate, 1)
	if ValidateWorkspaceVolumeObservation(create, pvcUID, "", "", nil) != nil {
		t.Fatal("first positive Create observation rejected")
	}
	for attempt := int32(2); attempt <= 5; attempt++ {
		if ValidateWorkspaceVolumeObservation(workspaceVolumeWorkFixture(WorkspaceVolumeCreate, attempt), pvcUID, "", "", nil) == nil {
			t.Fatal("Create reconciliation adopted a positive UID without the original response")
		}
	}
	for attempt := int32(1); attempt <= 5; attempt++ {
		work := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, attempt)
		ready := workspaceVolumeObservationFixture(work)
		if ValidateWorkspaceVolumeObservation(work, pvcUID, jobUID, podUID, &ready) != nil {
			t.Fatal("exact preparation observation rejected")
		}
	}
	recovery := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
	original := workspaceVolumeObservationFixture(recovery)
	recovery.TaskRef, recovery.Volume.ReadOnly, recovery.ReconcileOnly = "svt_recovery00", true, true
	recovery.InputDigest, _ = recovery.Digest()
	if ValidateWorkspaceVolumeObservation(recovery, pvcUID, jobUID, podUID, &original) != nil {
		t.Fatal("read-only recovery lost the immutable logical preparation")
	}
}

// TestWorkspaceVolumeObservationRejectsBorrowedFacts denies an unrelated physical object, preparation identity or malformed record.
// TestWorkspaceVolumeObservationRejectsBorrowedFacts отклоняет чужой physical object, preparation identity либо malformed record.
func TestWorkspaceVolumeObservationRejectsBorrowedFacts(t *testing.T) {
	for name, mutate := range map[string]func(*WorkspaceVolumeWork, *string, *string, **WorkspaceVolumeReady){
		"missing-pvc": func(_ *WorkspaceVolumeWork, pvc *string, _ *string, _ **WorkspaceVolumeReady) { *pvc = "" },
		"path-pvc":    func(_ *WorkspaceVolumeWork, pvc *string, _ *string, _ **WorkspaceVolumeReady) { *pvc = "../foreign" },
		"foreign-pvc": func(_ *WorkspaceVolumeWork, pvc *string, _ *string, _ **WorkspaceVolumeReady) {
			*pvc = "30000000-0000-4000-8000-000000000001"
		},
		"missing-pod":   func(_ *WorkspaceVolumeWork, _ *string, pod *string, _ **WorkspaceVolumeReady) { *pod = "" },
		"path-pod":      func(_ *WorkspaceVolumeWork, _ *string, pod *string, _ **WorkspaceVolumeReady) { *pod = "../foreign" },
		"missing-ready": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) { *ready = nil },
		"scope": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Preparation.Scope.ProjectRef = "prj_foreign000"
		},
		"work": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Preparation.WorkspaceRef = "wks_foreign000"
		},
		"task": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Preparation.TaskRef = "svt_foreign000"
		},
		"volume-generation": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Preparation.VolumeGeneration++
		},
		"record-pvc": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Preparation.PVCUID = "30000000-0000-4000-8000-000000000001"
		},
		"schema": func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) {
			(*ready).Schema = "unknown"
		},
		"device":           func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) { (*ready).Device = 0 },
		"inode":            func(_ *WorkspaceVolumeWork, _ *string, _ *string, ready **WorkspaceVolumeReady) { (*ready).Inode = 0 },
		"effective-digest": func(work *WorkspaceVolumeWork, _ *string, _ *string, _ **WorkspaceVolumeReady) { work.InputDigest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			work := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
			record := workspaceVolumeObservationFixture(work)
			ready := &record
			pvc, pod := work.Volume.PVCUID, "20000000-0000-4000-8000-000000000001"
			mutate(&work, &pvc, &pod, &ready)
			if ValidateWorkspaceVolumeObservation(work, pvc, "30000000-0000-4000-8000-000000000001", pod, ready) == nil {
				t.Fatal("borrowed or malformed preparation fact accepted")
			}
		})
	}
	create := workspaceVolumeWorkFixture(WorkspaceVolumeCreate, 1)
	preparation := workspaceVolumeWorkFixture(WorkspaceVolumePrepare, 1)
	ready := workspaceVolumeObservationFixture(preparation)
	if ValidateWorkspaceVolumeObservation(create, preparation.Volume.PVCUID, "", "", &ready) == nil ||
		ValidateWorkspaceVolumeObservation(create, preparation.Volume.PVCUID, "", "20000000-0000-4000-8000-000000000001", nil) == nil ||
		ValidateWorkspaceVolumeObservation(create, preparation.Volume.PVCUID, "30000000-0000-4000-8000-000000000001", "", nil) == nil {
		t.Fatal("Create borrowed a preparation report shape")
	}
	for _, badJob := range []string{"", "../foreign", "UID"} {
		if ValidateWorkspaceVolumeObservation(preparation, preparation.Volume.PVCUID, badJob, "20000000-0000-4000-8000-000000000001", &ready) == nil {
			t.Fatal("preparation report omitted its exact Job identity")
		}
	}
}
