package app

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"testing"
)

func TestContextHTTPMethodsHaveExactAuthorityProfile(t *testing.T) {
	operations := controlplaneclient.ControlAPIGatewayOperations()
	reverse := map[string]string{}
	for op, method := range operations {
		reverse[method] = op
	}
	methods := []string{
		controlplanev1.PlatformCommandService_SavePromptTemplateDraft_FullMethodName,
		controlplanev1.PlatformCommandService_DiscardPromptTemplateDraft_FullMethodName,
		controlplanev1.PlatformCommandService_SaveRoleImageRevisionDraft_FullMethodName,
		controlplanev1.PlatformCommandService_DiscardRoleImageRevisionDraft_FullMethodName,
		controlplanev1.PlatformCommandService_SaveIntegrationDefinitionDraft_FullMethodName,
		controlplanev1.PlatformCommandService_DiscardIntegrationDefinitionDraft_FullMethodName,
		controlplanev1.PlatformCommandService_SaveSystemSTTConfigurationDraft_FullMethodName,
		controlplanev1.PlatformCommandService_DiscardSystemSTTConfigurationDraft_FullMethodName,
		controlplanev1.PlatformQueryService_GetEmailEffectReceipt_FullMethodName,
		controlplanev1.PlatformCommandService_ReconcileEmailEffect_FullMethodName,
		controlplanev1.PlatformQueryService_ListSkillBundles_FullMethodName,
		controlplanev1.PlatformQueryService_GetSkillBundle_FullMethodName,
		controlplanev1.PlatformQueryService_ListSkillBundleRevisions_FullMethodName,
		controlplanev1.PlatformCommandService_ArchiveSkillBundle_FullMethodName,
		controlplanev1.PlatformCommandService_RestoreSkillBundle_FullMethodName,
		controlplanev1.PlatformCommandService_PurgeSkillBundle_FullMethodName,
		controlplanev1.PlatformCommandService_BindAgentSkillBundle_FullMethodName,
		controlplanev1.PlatformCommandService_UnbindAgentSkillBundle_FullMethodName,
		controlplanev1.PlatformQueryService_ListMemoryRecords_FullMethodName,
		controlplanev1.PlatformQueryService_GetMemoryRecord_FullMethodName,
		controlplanev1.PlatformQueryService_ListMemoryRecordRevisions_FullMethodName,
		controlplanev1.PlatformCommandService_ArchiveMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_RestoreMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_PurgeMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_BindAgentMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_UnbindAgentMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_CreateSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_SaveSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_ValidateSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_ReviewSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_PublishSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_DiscardSkillBundleDraft_FullMethodName,
		controlplanev1.PlatformCommandService_CreateMemoryRecord_FullMethodName,
		controlplanev1.PlatformCommandService_ReviseMemoryRecord_FullMethodName,
	}
	for _, method := range methods {
		op := reverse[method]
		if op == "" || authorityProofOperations()[op] != method {
			t.Fatalf("missing protected method %s", method)
		}
		_, requiresProject := authorityProjectRequiredOperations()[op]
		wantProject := method == controlplanev1.PlatformCommandService_CreateSkillBundleDraft_FullMethodName || method == controlplanev1.PlatformCommandService_CreateMemoryRecord_FullMethodName
		if requiresProject != wantProject {
			t.Fatalf("incorrect project authority scope for %s", method)
		}
	}
}
