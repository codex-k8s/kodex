package service

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func (s *Service) recordDecision(ctx context.Context, input CheckAccessInput, decision enum.AccessDecision, reasonCode string, rules []entity.AccessRule) (CheckAccessResult, error) {
	explanation := value.DecisionExplanation{
		Decision: string(decision), ReasonCode: reasonCode, PolicyVersion: policyVersion(rules),
		MatchedRules: ruleExplanations(rules, reasonCode),
	}
	now := s.now(input.Meta)
	audit := entity.AccessDecisionAudit{
		ID: s.ids.New(), Subject: input.Subject, ActionKey: input.ActionKey, Resource: input.Resource,
		Scope: input.Scope, RequestContext: input.Meta.RequestContext,
		Decision: decision, ReasonCode: reasonCode, PolicyVersion: explanation.PolicyVersion,
		Explanation: explanation, CreatedAt: now,
	}
	var event *entity.OutboxEvent
	if decision == enum.AccessDecisionDeny {
		evt, err := s.event(accessEventAccessDecisionRecorded, accessAggregateAccessDecisionAudit, audit.ID, value.AccessEventPayload{
			AccessDecisionAuditID: audit.ID.String(),
			SubjectType:           audit.Subject.Type,
			SubjectID:             audit.Subject.ID,
			ActionKey:             audit.ActionKey,
			Decision:              string(audit.Decision),
			ReasonCode:            audit.ReasonCode,
		}, now)
		if err != nil {
			return CheckAccessResult{}, err
		}
		event = &evt
	}
	if err := s.repository.RecordAccessDecision(ctx, audit, event); err != nil {
		return CheckAccessResult{}, err
	}
	return CheckAccessResult{Decision: decision, ReasonCode: reasonCode, Explanation: explanation}, nil
}
