package grpc

import (
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

// TestRuntimeWorkspaceVolumeReadySurvivesWire proves the owner record is present in the actual
// revision projection and cannot be replaced by a different workspace or tenant preparation.
// TestRuntimeWorkspaceVolumeReadySurvivesWire доказывает сохранение owner record в actual
// revision projection и запрещает замену preparation другого workspace либо tenant.
func TestRuntimeWorkspaceVolumeReadySurvivesWire(t *testing.T) {
	values := workspaceRevisionFixture()
	binding := values["workspaceStorage"].(runtimecontract.WorkspaceStorageBinding)
	binding.VolumeReady = &runtimecontract.WorkspaceVolumeReady{Schema: runtimecontract.WorkspaceVolumeReadySchema, Device: 31, Inode: 47,
		Preparation: runtimecontract.WorkspaceVolumePreparation{Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef,
			TaskRef: "svt_fixture000", PVCUID: "30000000-0000-4000-8000-000000000001", VolumeGeneration: 2}}
	values["workspaceStorage"] = binding
	result := castRuntimeRevision(values)
	if result == nil || result.GetWorkspaceStorage().GetVolumeReady() == nil {
		t.Fatal("accepted volume preparation was dropped from the runtime revision")
	}
	raw, err := proto.Marshal(result)
	if err != nil {
		t.Fatal("encode runtime revision", err)
	}
	decoded := result.ProtoReflect().New().Interface()
	if proto.Unmarshal(raw, decoded) != nil || !proto.Equal(result, decoded) {
		t.Fatal("accepted preparation changed in the protobuf roundtrip")
	}
	actual, valid := decodeWorkspaceVolumeReady(result.GetWorkspaceStorage().GetVolumeReady())
	if !valid || actual == nil || *actual != *binding.VolumeReady {
		t.Fatal("owner preparation fields changed in the transport projection")
	}
	for _, test := range []struct {
		name   string
		mutate func(*runtimecontract.WorkspaceVolumeReady)
	}{
		{"foreign_project", func(value *runtimecontract.WorkspaceVolumeReady) {
			value.Preparation.Scope.ProjectRef = "prj_foreign000"
		}},
		{"foreign_workspace", func(value *runtimecontract.WorkspaceVolumeReady) { value.Preparation.WorkspaceRef = "wks_foreign000" }},
		{"missing_pvc", func(value *runtimecontract.WorkspaceVolumeReady) { value.Preparation.PVCUID = "" }},
		{"missing_inode", func(value *runtimecontract.WorkspaceVolumeReady) { value.Inode = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := *binding.VolumeReady
			test.mutate(&changed)
			candidate := binding
			candidate.VolumeReady = &changed
			// Simulate the JSON-backed owner snapshot instead of passing the Go struct directly.
			// Используется JSON-backed owner snapshot вместо прямой передачи Go struct.
			raw, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot map[string]any
			if json.Unmarshal(raw, &snapshot) != nil {
				t.Fatal("decode owner snapshot")
			}
			values["workspaceStorage"] = snapshot
			if castRuntimeRevision(values) != nil {
				t.Fatal("invalid preparation was admitted or silently dropped")
			}
		})
	}
}

// workspaceRevisionFixture builds owner-selected identity and quotas for the real transport adapter.
// workspaceRevisionFixture создаёт owner-selected identity и quotas для настоящего transport adapter.
func workspaceRevisionFixture() map[string]any {
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	policy := runtimecontract.RuntimeWorkspacePolicyV1()
	domain := entity.RuntimeWorkspacePolicy{Revision: policy.Revision, Root: policy.Root,
		MaximumWritableBytes: policy.MaximumWritableBytes, MaximumFileCount: policy.MaximumFileCount,
		Digest: policy.Digest, DenialReasons: policy.DenialReasons}
	for _, rule := range policy.Rules {
		domain.Rules = append(domain.Rules, entity.RuntimeWorkspacePathRule{Path: rule.Path, Access: rule.Access})
	}
	return map[string]any{
		"organizationRef": scope.OrganizationRef, "projectRef": scope.ProjectRef,
		"sessionRef": scope.SessionRef, "agentRef": scope.AgentRef, "assistantScope": "NONE",
		"roleRuntimeContractRevision": runtimecontract.WorkspaceStorageRuntimeContractRevision,
		"workspacePolicy":             domain, "workspaceStorage": runtimecontract.WorkspaceStorageBinding{
			Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 1},
	}
}

// TestRuntimeWorkspaceRevisionAdmission preserves the assigned ABI through full owner-to-Proto casting.
// Legacy omission remains valid; assigned invalid V2 metadata never silently downgrades to legacy.
// TestRuntimeWorkspaceRevisionAdmission сохраняет назначенный ABI через полный owner-to-Proto caster.
// Legacy omission допустим; invalid assigned V2 metadata не понижается молча до legacy.
func TestRuntimeWorkspaceRevisionAdmission(t *testing.T) {
	t.Run("exact_v2", func(t *testing.T) {
		values := workspaceRevisionFixture()
		result := castRuntimeRevision(values)
		if result == nil || result.WorkspaceStorage == nil || result.RoleRuntimeContractRevision != runtimecontract.WorkspaceStorageRuntimeContractRevision {
			t.Fatal("valid workspace rejected or runtime contract revision lost")
		}
		encoded, err := proto.Marshal(result)
		if err != nil {
			t.Fatal("marshal workspace revision", err)
		}
		decoded := result.ProtoReflect().New().Interface()
		if proto.Unmarshal(encoded, decoded) != nil || !proto.Equal(result, decoded) ||
			result.WorkspaceStorage.WorkspaceRef != "wks_fixture000" || result.WorkspaceStorage.Scope.ProjectRef != "prj_fixture000" || result.WorkspaceStorage.Generation != 1 {
			t.Fatal("workspace scope or generation lost in protobuf roundtrip")
		}
	})
	for _, test := range []struct {
		name  string
		field string
		value any
	}{
		{"old_contract", "roleRuntimeContractRevision", uint64(3)},
		{"absent_contract", "roleRuntimeContractRevision", nil},
		{"unknown_contract", "roleRuntimeContractRevision", uint64(5)},
		{"foreign_project", "projectRef", "prj_foreign000"},
		{"foreign_session", "sessionRef", "ses_foreign000"},
		{"foreign_agent", "agentRef", "agt_foreign000"},
		{"assistant", "assistantScope", "PROJECT"},
		{"null_binding", "workspaceStorage", nil},
		{"missing_policy", "workspacePolicy", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := workspaceRevisionFixture()
			values[test.field] = test.value
			if castRuntimeRevision(values) != nil {
				t.Fatal("invalid assigned workspace was accepted or downgraded")
			}
		})
	}
	t.Run("legacy_without_binding", func(t *testing.T) {
		values := workspaceRevisionFixture()
		delete(values, "workspaceStorage")
		values["roleRuntimeContractRevision"] = uint64(3)
		result := castRuntimeRevision(values)
		if result == nil || result.WorkspaceStorage != nil || result.RoleRuntimeContractRevision != 3 {
			t.Fatal("legacy revision was upgraded or rejected")
		}
	})
}
