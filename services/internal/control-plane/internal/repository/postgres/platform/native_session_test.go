package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestNativePendingEffectsWaitOnly verifies that pending effects preserve the queue while
// checkpoint/authority conflicts, cancellation and infrastructure retain their original treatment.
// TestNativePendingEffectsWaitOnly проверяет сохранение очереди при pending effects;
// checkpoint/authority conflicts, отмена и infrastructure сохраняют прежнее поведение.
func TestNativePendingEffectsWaitOnly(t *testing.T) {
	pending := runtimeEligibilityDiagnostic(runtimeReasonSessionResume, errNativeSessionEffectsPending)
	if !runtimeSessionEffectsPending(fmt.Errorf("producer: %w", pending)) || runtimeCandidateEligibilityFailure(pending) {
		t.Fatal("pending external effects became terminal input failure")
	}
	for _, cause := range []error{errs.ErrConflict, errs.ErrForbidden, errs.ErrInvalid, errs.ErrUnavailable,
		context.Canceled, context.DeadlineExceeded, &pgconn.PgError{Code: "40001"}} {
		if runtimeSessionEffectsPending(cause) || runtimeSessionEffectsPending(errors.Join(pending, cause)) {
			t.Fatal("an unrelated failure was hidden as pending effects")
		}
	}
	if !runtimeCandidateEligibilityFailure(runtimeEligibilityDiagnostic(runtimeReasonSessionResume, errs.ErrConflict)) {
		t.Fatal("native checkpoint mismatch lost terminal eligibility protection")
	}
}

// nativeResumeSnapshot creates independent synthetic scope/provider/initiator maps without credentials.
// nativeResumeSnapshot создаёт независимые synthetic scope/provider/initiator maps без credentials.
func nativeResumeSnapshot() map[string]any {
	profile := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "synthetic", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.40/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	return map[string]any{"runtimeProvider": "opencode", "organizationRef": "org_synthetic", "projectRef": "prj_synthetic", "sessionRef": "ses_synthetic", "agentRef": "agt_synthetic", "providerAccountRef": "pacc_synthetic", "runtimeModel": "model-a", "runtimeKey": "runtime_synthetic", "assistantScope": "NONE", "assistantProfileRef": "", "roleRuntimeContractRevision": int64(runtimecontract.WorkspaceStorageRuntimeContractRevision), "modelProvider": profile, "promptSnapshot": map[string]any{"variables": map[string]any{"user.ref": "usr_synthetic"}}}
}

// TestNativeResumeRefreshesAuthorityWithoutDiscardingHistory accepts new instructions and credential revisions
// under the same persistent owner boundary; these values are resolved again in the new RuntimeRevision.
// TestNativeResumeRefreshesAuthorityWithoutDiscardingHistory принимает новые инструкции и credential revisions
// в той же persistent owner boundary; значения заново разрешаются в новой RuntimeRevision.
func TestNativeResumeRefreshesAuthorityWithoutDiscardingHistory(t *testing.T) {
	previous, current := nativeResumeSnapshot(), nativeResumeSnapshot()
	previous["instructionRef"], current["instructionRef"] = "instr_old", "instr_new"
	previous["providerCredentialRevisionRef"], current["providerCredentialRevisionRef"] = "pcr_old", "pcr_new"
	previous["capabilities"], current["capabilities"] = []string{"platform.run.delegate", "platform.artifact.manage"}, []string{"platform.run.delegate"}
	profile := current["modelProvider"].(runtimecontract.ModelProviderConfiguration)
	profile.SourceRevision = strings.Repeat("b", 40)
	current["modelProvider"] = profile
	if !nativeSessionResumeCompatible(previous, current) {
		t.Fatal("fresh authority or credential materialization discarded native history")
	}
}

// TestNativeResumeAcceptsTypedCurrentPrompt covers the actual claim producer and persisted JSON reader shapes.
// Equal trusted initiator identity must not be lost because the current snapshot still holds a typed DTO.
// TestNativeResumeAcceptsTypedCurrentPrompt проверяет actual claim producer и persisted JSON reader shapes.
// Trusted initiator identity не теряется из-за typed DTO в текущем snapshot.
func TestNativeResumeAcceptsTypedCurrentPrompt(t *testing.T) {
	previous, current := nativeResumeSnapshot(), nativeResumeSnapshot()
	current["promptSnapshot"] = entity.PromptMaterializationSnapshot{Variables: map[string]string{"user.ref": "usr_synthetic"}}
	if !nativeSessionResumeCompatible(previous, current) {
		t.Fatal("typed producer lost native recovery identity")
	}
	current["promptSnapshot"] = entity.PromptMaterializationSnapshot{Variables: map[string]string{"user.ref": "usr_foreign"}}
	if nativeSessionResumeCompatible(previous, current) {
		t.Fatal("typed foreign initiator accepted")
	}
}

// TestNativeResumeRejectsForeignScopeAndUnknownCompatibility blocks silent cold-start or foreign reuse.
// TestNativeResumeRejectsForeignScopeAndUnknownCompatibility запрещает неявный cold start или чужой reuse.
func TestNativeResumeRejectsForeignScopeAndUnknownCompatibility(t *testing.T) {
	for _, key := range []string{"organizationRef", "projectRef", "sessionRef", "agentRef", "providerAccountRef", "runtimeModel", "runtimeKey", "assistantScope", "assistantProfileRef"} {
		t.Run(key, func(t *testing.T) {
			previous, current := nativeResumeSnapshot(), nativeResumeSnapshot()
			current[key] = "foreign"
			if nativeSessionResumeCompatible(previous, current) {
				t.Fatal("foreign native scope accepted")
			}
		})
	}
	for _, test := range []string{"initiator", "route", "provider-id", "missing-provider", "abi"} {
		t.Run(test, func(t *testing.T) {
			previous, current := nativeResumeSnapshot(), nativeResumeSnapshot()
			switch test {
			case "initiator":
				current["promptSnapshot"].(map[string]any)["variables"].(map[string]any)["user.ref"] = "usr_foreign"
			case "route", "provider-id":
				profile := current["modelProvider"].(runtimecontract.ModelProviderConfiguration)
				if test == "route" {
					profile.BaseURL = "https://other.example.invalid/v1"
				} else {
					profile.ProviderID = "different"
				}
				current["modelProvider"] = profile
			case "missing-provider":
				delete(current, "modelProvider")
			case "abi":
				current["roleRuntimeContractRevision"] = int64(999)
			}
			if nativeSessionResumeCompatible(previous, current) {
				t.Fatal("unknown native compatibility accepted")
			}
		})
	}
}
