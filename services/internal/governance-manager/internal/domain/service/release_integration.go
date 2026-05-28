package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const releaseIntegrationDiagnosticExplicitRef = "explicit_ref_unvalidated"

func (s *Service) enrichReleaseIntegrationRefs(ctx context.Context, refs []value.ReleaseIntegrationRef) ([]value.ReleaseIntegrationRef, error) {
	enriched := make([]value.ReleaseIntegrationRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Domain != "governance" {
			item := enrichExternalReleaseIntegrationRef(ref)
			if err := validateReleaseIntegrationRef(item); err != nil {
				return nil, err
			}
			enriched = append(enriched, item)
			continue
		}
		id, err := uuid.Parse(ref.Ref)
		if err != nil {
			return nil, errs.ErrInvalidArgument
		}
		snapshot, err := s.localReleaseIntegrationSnapshot(ctx, ref, id)
		if err != nil {
			return nil, err
		}
		item, err := mergeReleaseIntegrationSnapshot(ref, snapshot)
		if err != nil {
			return nil, err
		}
		if err := validateReleaseIntegrationRef(item); err != nil {
			return nil, err
		}
		enriched = append(enriched, item)
	}
	return enriched, nil
}

func (s *Service) localReleaseIntegrationSnapshot(ctx context.Context, ref value.ReleaseIntegrationRef, id uuid.UUID) (value.ReleaseIntegrationRef, error) {
	switch ref.Kind {
	case "risk_assessment":
		assessment, err := s.repository.GetRiskAssessment(ctx, id)
		if err != nil {
			return value.ReleaseIntegrationRef{}, err
		}
		return riskAssessmentIntegrationSnapshot(ref, assessment), nil
	case "review_signal":
		signal, err := s.repository.GetReviewSignal(ctx, id)
		if err != nil {
			return value.ReleaseIntegrationRef{}, err
		}
		return reviewSignalIntegrationSnapshot(ref, signal), nil
	case "gate_request":
		request, err := s.repository.GetGateRequest(ctx, id)
		if err != nil {
			return value.ReleaseIntegrationRef{}, err
		}
		return gateRequestIntegrationSnapshot(ref, request), nil
	case "gate_decision":
		decision, err := s.repository.GetGateDecision(ctx, id)
		if err != nil {
			return value.ReleaseIntegrationRef{}, err
		}
		return gateDecisionIntegrationSnapshot(ref, decision), nil
	case "release_decision_package":
		item, err := s.repository.GetReleaseDecisionPackage(ctx, id)
		if err != nil {
			return value.ReleaseIntegrationRef{}, err
		}
		return releaseDecisionPackageIntegrationSnapshot(ref, item), nil
	default:
		return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
	}
}

func enrichExternalReleaseIntegrationRef(ref value.ReleaseIntegrationRef) value.ReleaseIntegrationRef {
	result := ref
	if result.Summary == "" {
		result.Summary = fmt.Sprintf("%s: %s %s explicit ref retained; owner read client not connected", releaseIntegrationDiagnosticExplicitRef, result.Domain, result.Kind)
	}
	return result
}

func mergeReleaseIntegrationSnapshot(ref value.ReleaseIntegrationRef, snapshot value.ReleaseIntegrationRef) (value.ReleaseIntegrationRef, error) {
	result := ref
	if snapshot.Status != "" {
		if result.Status != "" && result.Status != snapshot.Status {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.Status = snapshot.Status
	}
	if snapshot.Summary != "" {
		if result.Summary != "" && result.Summary != snapshot.Summary {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.Summary = snapshot.Summary
	}
	if snapshot.Digest != "" {
		if result.Digest != "" && result.Digest != snapshot.Digest {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.Digest = snapshot.Digest
	}
	if snapshot.ObservedAt != "" {
		if result.ObservedAt != "" && result.ObservedAt != snapshot.ObservedAt {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.ObservedAt = snapshot.ObservedAt
	}
	if snapshot.Version != "" {
		if result.Version != "" && result.Version != snapshot.Version {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.Version = snapshot.Version
	}
	if snapshot.ErrorCode != "" {
		if result.ErrorCode != "" && result.ErrorCode != snapshot.ErrorCode {
			return value.ReleaseIntegrationRef{}, errs.ErrInvalidArgument
		}
		result.ErrorCode = snapshot.ErrorCode
	}
	return result, nil
}

func riskAssessmentIntegrationSnapshot(ref value.ReleaseIntegrationRef, assessment entity.RiskAssessment) value.ReleaseIntegrationRef {
	return value.ReleaseIntegrationRef{
		Domain:     ref.Domain,
		Kind:       ref.Kind,
		Ref:        ref.Ref,
		Status:     string(assessment.Status),
		Summary:    localIntegrationSummary("risk assessment", string(assessment.Status), string(assessment.EffectiveRiskClass)),
		Digest:     releaseIntegrationDigest(ref.Domain, ref.Kind, ref.Ref, string(assessment.Status), string(assessment.EffectiveRiskClass), fmt.Sprint(assessment.Version)),
		ObservedAt: releaseIntegrationObservedAt(assessment.UpdatedAt, assessment.CreatedAt),
		Version:    releaseIntegrationVersion(assessment.Version),
	}
}

func reviewSignalIntegrationSnapshot(ref value.ReleaseIntegrationRef, signal entity.ReviewSignal) value.ReleaseIntegrationRef {
	return value.ReleaseIntegrationRef{
		Domain:     ref.Domain,
		Kind:       ref.Kind,
		Ref:        ref.Ref,
		Status:     string(signal.Outcome),
		Summary:    localIntegrationSummary("review signal", string(signal.Outcome), string(signal.Severity)),
		Digest:     releaseIntegrationDigest(ref.Domain, ref.Kind, ref.Ref, string(signal.Outcome), string(signal.Severity), signal.CreatedAt.UTC().Format(time.RFC3339Nano)),
		ObservedAt: releaseIntegrationObservedAt(signal.CreatedAt),
	}
}

func gateRequestIntegrationSnapshot(ref value.ReleaseIntegrationRef, request entity.GateRequest) value.ReleaseIntegrationRef {
	return value.ReleaseIntegrationRef{
		Domain:     ref.Domain,
		Kind:       ref.Kind,
		Ref:        ref.Ref,
		Status:     string(request.Status),
		Summary:    localIntegrationSummary("gate request", string(request.Status), request.Target.Type),
		Digest:     releaseIntegrationDigest(ref.Domain, ref.Kind, ref.Ref, string(request.Status), request.Target.Type, fmt.Sprint(request.Version)),
		ObservedAt: releaseIntegrationObservedAt(request.UpdatedAt, request.CreatedAt),
		Version:    releaseIntegrationVersion(request.Version),
	}
}

func gateDecisionIntegrationSnapshot(ref value.ReleaseIntegrationRef, decision entity.GateDecision) value.ReleaseIntegrationRef {
	return value.ReleaseIntegrationRef{
		Domain:     ref.Domain,
		Kind:       ref.Kind,
		Ref:        ref.Ref,
		Status:     string(decision.Outcome),
		Summary:    localIntegrationSummary("gate decision", string(decision.Outcome), ""),
		Digest:     releaseIntegrationDigest(ref.Domain, ref.Kind, ref.Ref, string(decision.Outcome), decision.DecidedAt.UTC().Format(time.RFC3339Nano)),
		ObservedAt: releaseIntegrationObservedAt(decision.DecidedAt),
	}
}

func releaseDecisionPackageIntegrationSnapshot(ref value.ReleaseIntegrationRef, item entity.ReleaseDecisionPackage) value.ReleaseIntegrationRef {
	return value.ReleaseIntegrationRef{
		Domain:     ref.Domain,
		Kind:       ref.Kind,
		Ref:        ref.Ref,
		Status:     string(item.Status),
		Summary:    localIntegrationSummary("release package", string(item.Status), item.ReleaseCandidateRef),
		Digest:     releaseIntegrationDigest(ref.Domain, ref.Kind, ref.Ref, string(item.Status), item.ReleaseCandidateRef, fmt.Sprint(item.Version)),
		ObservedAt: releaseIntegrationObservedAt(item.UpdatedAt, item.CreatedAt),
		Version:    releaseIntegrationVersion(item.Version),
	}
}

func localIntegrationSummary(parts ...string) string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		result = append(result, normalized)
	}
	return strings.Join(result, " ")
}

func releaseIntegrationVersion(version int64) string {
	if version <= 0 {
		return ""
	}
	return fmt.Sprint(version)
}

func releaseIntegrationObservedAt(values ...time.Time) string {
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		return value.UTC().Format(time.RFC3339)
	}
	return ""
}

func releaseIntegrationDigest(parts ...string) string {
	payload, _ := json.Marshal(parts)
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
}
