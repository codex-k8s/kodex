package service

import "errors"

const (
	SelfDeployGateRecoveryCodePlanLookupFailed                = "plan_lookup_failed"
	SelfDeployGateRecoveryCodeGateReplayFailed                = "gate_replay_failed"
	SelfDeployGateRecoveryCodeGatePrepareFailed               = "gate_prepare_failed"
	SelfDeployGateRecoveryCodeRiskEvaluationFailed            = "risk_evaluation_failed"
	SelfDeployGateRecoveryCodeGateResponseInvalid             = "gate_response_invalid"
	SelfDeployGateRecoveryCodeExistingRiskLookupFailed        = "existing_risk_lookup_failed"
	SelfDeployGateRecoveryCodeExistingRiskNotFound            = "existing_risk_not_found"
	SelfDeployGateRecoveryCodeExistingRiskFingerprintMismatch = "existing_risk_fingerprint_mismatch"
	SelfDeployGateRecoveryCodeExistingRiskConflict            = "existing_risk_conflict"
	SelfDeployGateRecoveryCodeExistingGateLookupFailed        = "existing_gate_lookup_failed"
	SelfDeployGateRecoveryCodeExistingGateNotFound            = "existing_gate_not_found"
	SelfDeployGateRecoveryCodeExistingGateMismatch            = "existing_gate_mismatch"
	SelfDeployGateRecoveryCodeExistingGateConflict            = "existing_gate_conflict"
	SelfDeployGateRecoveryCodePlanRefsUpdateFailed            = "plan_refs_update_failed"
	SelfDeployGateRecoveryCodePlanGovernanceRefsUpdateFailed  = "plan_governance_refs_update_failed"
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
