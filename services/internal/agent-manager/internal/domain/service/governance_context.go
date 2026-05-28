package service

import (
	"strings"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func normalizeGovernanceContext(context value.GovernanceContextRef) (value.GovernanceContextRef, error) {
	refs := []struct {
		value *string
	}{
		{&context.RiskAssessmentRef},
		{&context.GateRequestRef},
		{&context.GateDecisionRef},
		{&context.ReleaseDecisionPackageRef},
		{&context.ReleaseDecisionRef},
		{&context.RiskProfileRef},
		{&context.GatePolicyRef},
		{&context.ReleasePolicyRef},
	}
	for _, ref := range refs {
		normalized, err := normalizeGovernanceRef(*ref.value)
		if err != nil {
			return value.GovernanceContextRef{}, err
		}
		*ref.value = normalized
	}
	if context.GateDecisionRef != "" && context.GateRequestRef == "" {
		return value.GovernanceContextRef{}, errs.ErrInvalidArgument
	}
	if context.ReleaseDecisionRef != "" && context.ReleaseDecisionPackageRef == "" {
		return value.GovernanceContextRef{}, errs.ErrInvalidArgument
	}
	return context, nil
}

func normalizeGovernanceRef(ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", nil
	}
	if normalized, err := normalizeFollowUpOptionalRef(trimmed); err != nil || normalized == "" {
		return "", errs.ErrInvalidArgument
	} else {
		return normalized, nil
	}
}

func mergeGovernanceContext(stored value.GovernanceContextRef, incoming value.GovernanceContextRef) (value.GovernanceContextRef, error) {
	var err error
	stored.RiskAssessmentRef, err = mergeGovernanceRef(stored.RiskAssessmentRef, incoming.RiskAssessmentRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.GateRequestRef, err = mergeGovernanceRef(stored.GateRequestRef, incoming.GateRequestRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.GateDecisionRef, err = mergeGovernanceRef(stored.GateDecisionRef, incoming.GateDecisionRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.ReleaseDecisionPackageRef, err = mergeGovernanceRef(stored.ReleaseDecisionPackageRef, incoming.ReleaseDecisionPackageRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.ReleaseDecisionRef, err = mergeGovernanceRef(stored.ReleaseDecisionRef, incoming.ReleaseDecisionRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.RiskProfileRef, err = mergeGovernanceRef(stored.RiskProfileRef, incoming.RiskProfileRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.GatePolicyRef, err = mergeGovernanceRef(stored.GatePolicyRef, incoming.GatePolicyRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	stored.ReleasePolicyRef, err = mergeGovernanceRef(stored.ReleasePolicyRef, incoming.ReleasePolicyRef)
	if err != nil {
		return value.GovernanceContextRef{}, err
	}
	return normalizeGovernanceContext(stored)
}

func mergeGovernanceRef(stored string, incoming string) (string, error) {
	stored = strings.TrimSpace(stored)
	incoming = strings.TrimSpace(incoming)
	switch {
	case stored == "":
		return incoming, nil
	case incoming == "", stored == incoming:
		return stored, nil
	default:
		return "", errs.ErrConflict
	}
}

func sameGovernanceContext(left value.GovernanceContextRef, right value.GovernanceContextRef) bool {
	return strings.TrimSpace(left.RiskAssessmentRef) == strings.TrimSpace(right.RiskAssessmentRef) &&
		strings.TrimSpace(left.GateRequestRef) == strings.TrimSpace(right.GateRequestRef) &&
		strings.TrimSpace(left.GateDecisionRef) == strings.TrimSpace(right.GateDecisionRef) &&
		strings.TrimSpace(left.ReleaseDecisionPackageRef) == strings.TrimSpace(right.ReleaseDecisionPackageRef) &&
		strings.TrimSpace(left.ReleaseDecisionRef) == strings.TrimSpace(right.ReleaseDecisionRef) &&
		strings.TrimSpace(left.RiskProfileRef) == strings.TrimSpace(right.RiskProfileRef) &&
		strings.TrimSpace(left.GatePolicyRef) == strings.TrimSpace(right.GatePolicyRef) &&
		strings.TrimSpace(left.ReleasePolicyRef) == strings.TrimSpace(right.ReleasePolicyRef)
}

func governanceContextWithGateRefs(context value.GovernanceContextRef, gateRequestRef string, gateDecisionRef string) (value.GovernanceContextRef, error) {
	incoming := value.GovernanceContextRef{
		GateRequestRef:  gateRequestRef,
		GateDecisionRef: gateDecisionRef,
	}
	return mergeGovernanceContext(context, incoming)
}
