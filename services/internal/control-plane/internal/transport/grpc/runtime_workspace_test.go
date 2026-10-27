package grpc

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/proto"
)

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
