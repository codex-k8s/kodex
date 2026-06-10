package service

import "errors"

const (
	SelfDeployGateRecoveryCodePlanLookupFailed               = "plan_lookup_failed"
	SelfDeployGateRecoveryCodeGateReplayFailed               = "gate_replay_failed"
	SelfDeployGateRecoveryCodeGatePrepareFailed              = "gate_prepare_failed"
	SelfDeployGateRecoveryCodeRiskEvaluationFailed           = "risk_evaluation_failed"
	SelfDeployGateRecoveryCodeGateResponseInvalid            = "gate_response_invalid"
	SelfDeployGateRecoveryCodeExistingGateLookupFailed       = "existing_gate_lookup_failed"
	SelfDeployGateRecoveryCodePlanRefsUpdateFailed           = "plan_refs_update_failed"
	SelfDeployGateRecoveryCodePlanGovernanceRefsUpdateFailed = "plan_governance_refs_update_failed"
)

type selfDeployGateRecoveryError struct {
	code string
	err  error
}

func (e selfDeployGateRecoveryError) Error() string {
	return e.code
}

func (e selfDeployGateRecoveryError) Unwrap() error {
	return e.err
}

func selfDeployGateRecoveryErrorf(code string, err error) error {
	if err == nil {
		return nil
	}
	return selfDeployGateRecoveryError{code: code, err: err}
}

func NewSelfDeployGateRecoveryError(code string, err error) error {
	return selfDeployGateRecoveryErrorf(code, err)
}

func SelfDeployGateRecoveryErrorCode(err error) string {
	var stage selfDeployGateRecoveryError
	if errors.As(err, &stage) {
		return stage.code
	}
	return ""
}
