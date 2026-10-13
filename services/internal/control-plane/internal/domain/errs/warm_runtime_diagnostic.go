package errs

import "errors"

const (
	WarmReconcileScope                 = "SCOPE"
	WarmReconcileTransaction           = "TRANSACTION"
	WarmReconcileProviderPolicy        = "PROVIDER_POLICY"
	WarmReconcileProviderPolicyRead    = "PROVIDER_POLICY_READ"
	WarmReconcileProviderPolicyDecode  = "PROVIDER_POLICY_DECODE"
	WarmReconcileProviderPolicyCatalog = "PROVIDER_POLICY_CATALOG"
	WarmReconcileProviderPolicyPublish = "PROVIDER_POLICY_PUBLISH"
	WarmReconcileSessionBinding        = "SESSION_BINDING"
	WarmReconcileSessionReplacement    = "SESSION_REPLACEMENT"
	WarmReconcileSnapshot              = "RUNTIME_SNAPSHOT"
	WarmReconcileOverlay               = "OVERLAY"
	WarmReconcileCatalog               = "SESSION_CATALOG"
	WarmReconcileEnvironment           = "ENVIRONMENT"
	WarmReconcileLifecycle             = "LIFECYCLE"
	WarmReconcilePrompt                = "PROMPT"
	WarmReconcileWorkspace             = "WORKSPACE"
	WarmReconcileContext               = "CONTEXT"
	WarmReconcileSpecification         = "SPECIFICATION"
	WarmReconcileDigest                = "DIGEST"
	WarmReconcileCommit                = "COMMIT"
)

type warmRuntimeFailure struct {
	stage string
	cause error
}

func (failure *warmRuntimeFailure) Error() string { return "warm runtime reconciliation failed" }
func (failure *warmRuntimeFailure) Unwrap() error { return failure.cause }

// Маркер не меняет errors.Is/As, транзакцию либо публичный статус. Более точный
// уже назначенный сервером этап сохраняется; строки adapter не отражаются.
func WithWarmRuntimeStage(err error, stage string) error {
	if err == nil {
		return nil
	}
	if WarmRuntimeStage(err) != "UNKNOWN" {
		return err
	}
	return &warmRuntimeFailure{stage: closedWarmRuntimeStage(stage), cause: err}
}

func WarmRuntimeStage(err error) string {
	var failure *warmRuntimeFailure
	if errors.As(err, &failure) && failure != nil {
		return closedWarmRuntimeStage(failure.stage)
	}
	return "UNKNOWN"
}

func closedWarmRuntimeStage(stage string) string {
	switch stage {
	case WarmReconcileScope, WarmReconcileTransaction, WarmReconcileProviderPolicy,
		WarmReconcileProviderPolicyRead, WarmReconcileProviderPolicyDecode,
		WarmReconcileProviderPolicyCatalog, WarmReconcileProviderPolicyPublish,
		WarmReconcileSessionBinding, WarmReconcileSessionReplacement,
		WarmReconcileSnapshot, WarmReconcileOverlay, WarmReconcileCatalog,
		WarmReconcileEnvironment, WarmReconcileLifecycle, WarmReconcilePrompt,
		WarmReconcileWorkspace, WarmReconcileContext, WarmReconcileSpecification,
		WarmReconcileDigest, WarmReconcileCommit:
		return stage
	default:
		return "UNKNOWN"
	}
}
