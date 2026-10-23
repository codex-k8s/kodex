package errs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestWarmRuntimeStagePreservesCauseAndClosedDiagnostics checks all twenty warm stages against five error classes, private-message suppression and preservation of an inner stage by outer wrapping. Nil and unknown-stage examples retain safe outcomes; this is an in-memory wrapper test, not runtime reconciliation.
// TestWarmRuntimeStagePreservesCauseAndClosedDiagnostics проверяет все двадцать этапов тёплого runtime с пятью классами ошибок, скрытие закрытого сообщения и сохранение внутреннего этапа внешней обёрткой. Nil и примеры неизвестных этапов сохраняют безопасные исходы; это тест обёрток в памяти, а не сверка runtime.
func TestWarmRuntimeStagePreservesCauseAndClosedDiagnostics(t *testing.T) {
	for _, stage := range []string{WarmReconcileScope, WarmReconcileTransaction, WarmReconcileProviderPolicy,
		WarmReconcileProviderPolicyRead, WarmReconcileProviderPolicyDecode, WarmReconcileProviderPolicyCatalog,
		WarmReconcileProviderPolicyPublish, WarmReconcileSessionBinding, WarmReconcileSessionReplacement,
		WarmReconcileSnapshot, WarmReconcileOverlay, WarmReconcileCatalog, WarmReconcileEnvironment,
		WarmReconcileLifecycle, WarmReconcilePrompt, WarmReconcileWorkspace, WarmReconcileContext,
		WarmReconcileSpecification, WarmReconcileDigest, WarmReconcileCommit} {
		t.Run(stage, func(t *testing.T) {
			for _, cause := range []error{ErrConflict, ErrVersionMismatch, ErrUnavailable, ErrForbidden, ErrInvalid} {
				err := WithWarmRuntimeStage(fmt.Errorf("PRIVATE_SENTINEL: %w", cause), stage)
				if !errors.Is(err, cause) || WarmRuntimeStage(err) != stage || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("stage changed authority class or reflected a private diagnostic")
				}
				wrapped := WithWarmRuntimeStage(err, WarmReconcileScope)
				if wrapped != err || WarmRuntimeStage(wrapped) != stage {
					t.Fatal("outer marker lost the exact rejecting stage")
				}
			}
		})
	}
	if WithWarmRuntimeStage(nil, WarmReconcileScope) != nil || WarmRuntimeStage(nil) != "UNKNOWN" {
		t.Fatal("success acquired a failure marker")
	}
	for _, err := range []error{errors.New("PRIVATE_STAGE"), WithWarmRuntimeStage(ErrConflict, "PRIVATE_STAGE"), &warmRuntimeFailure{stage: "PRIVATE_STAGE", cause: ErrConflict}} {
		if WarmRuntimeStage(err) != "UNKNOWN" {
			t.Fatal("unknown stage reflected private text")
		}
	}
}
