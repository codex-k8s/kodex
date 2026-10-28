package workload

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
	"k8s.io/client-go/kubernetes/fake"
)

// TestWorkspaceVolumeReadyReachesRunnerInput traverses the real turn-input builder, including
// immutable JSON encoding, so a caster that silently drops the accepted record cannot pass.
// TestWorkspaceVolumeReadyReachesRunnerInput проходит actual turn-input builder и immutable
// JSON encoding: caster, молча отбрасывающий accepted record, проверку не проходит.
func TestWorkspaceVolumeReadyReachesRunnerInput(t *testing.T) {
	manager := newTestManager(t, fake.NewSimpleClientset())
	manager.config.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	execution := testExecution(false)
	revision := execution.Revision
	revision.SessionRef, revision.AgentRef = "ses_workspace000", "agt_workspace000"
	revision.RoleRuntimeContractRevision = runtimecontract.WorkspaceStorageRuntimeContractRevision
	scope := &cp.RuntimeWorkspaceScope{OrganizationRef: revision.OrganizationRef, ProjectRef: execution.Run.ProjectRef,
		SessionRef: revision.SessionRef, AgentRef: revision.AgentRef}
	revision.WorkspaceStorage = &cp.RuntimeWorkspaceStorage{Profile: runtimecontract.WorkspaceStorageProfile,
		Scope: scope, WorkspaceRef: "wks_workspace000", Generation: 1,
		VolumeReady: &cp.RuntimeWorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: 31, Inode: 47,
			Preparation: &cp.RuntimeWorkspaceVolumePreparation{Scope: proto.Clone(scope).(*cp.RuntimeWorkspaceScope),
				WorkspaceRef: "wks_workspace000", TaskRef: "svt_workspace000", PvcUid: "30000000-0000-4000-8000-000000000001", VolumeGeneration: 2}}}
	sealTestTurnExecution(execution)
	input, _, err := manager.BuildTurnInput(execution)
	if err != nil || input.WorkspaceStorage == nil || input.WorkspaceStorage.VolumeReady == nil {
		t.Fatalf("accepted preparation was dropped from runner input: %v", err)
	}
	ready := input.WorkspaceStorage.VolumeReady
	if ready.Device != 31 || ready.Inode != 47 || ready.Preparation.TaskRef != "svt_workspace000" ||
		ready.Preparation.PVCUID != revision.WorkspaceStorage.VolumeReady.Preparation.PvcUid || ready.Preparation.VolumeGeneration != 2 {
		t.Fatal("sealed volume fields changed in runner input")
	}
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		t.Fatal("encode immutable runner input", err)
	}
	decoded, err := runtimecontract.DecodeRunnerInput(raw)
	if err != nil || decoded.WorkspaceStorage == nil || decoded.WorkspaceStorage.VolumeReady == nil ||
		*decoded.WorkspaceStorage.VolumeReady != *ready {
		t.Fatal("sealed preparation changed in immutable runner input")
	}
	for _, test := range []struct {
		name   string
		mutate func(*cp.RuntimeWorkspaceStorage)
	}{
		{"storage_unknown", func(value *cp.RuntimeWorkspaceStorage) { value.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01}) }},
		{"scope_unknown", func(value *cp.RuntimeWorkspaceStorage) {
			value.Scope.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		}},
		{"ready_unknown", func(value *cp.RuntimeWorkspaceStorage) {
			value.VolumeReady.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		}},
		{"preparation_unknown", func(value *cp.RuntimeWorkspaceStorage) {
			value.VolumeReady.Preparation.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		}},
		{"preparation_scope_unknown", func(value *cp.RuntimeWorkspaceStorage) {
			value.VolumeReady.Preparation.Scope.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
		}},
		{"missing_prep", func(value *cp.RuntimeWorkspaceStorage) { value.VolumeReady.Preparation = nil }},
		{"foreign_prep", func(value *cp.RuntimeWorkspaceStorage) {
			value.VolumeReady.Preparation.Scope.ProjectRef = "prj_foreign000"
		}},
		{"foreign_work", func(value *cp.RuntimeWorkspaceStorage) { value.VolumeReady.Preparation.WorkspaceRef = "wks_foreign000" }},
		{"missing_inode", func(value *cp.RuntimeWorkspaceStorage) { value.VolumeReady.Inode = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := proto.Clone(execution).(*cp.ClaimedExecution)
			test.mutate(candidate.Revision.WorkspaceStorage)
			binding, err := workspaceStorageFromProto(candidate.Revision.WorkspaceStorage)
			if err == nil && binding != nil && binding.ValidateForInput(input) == nil {
				t.Fatal("invalid preparation passed the storage boundary before digest validation")
			}
			if _, _, err := manager.BuildTurnInput(candidate); err == nil {
				t.Fatal("invalid preparation was admitted or silently discarded")
			}
		})
	}
}
