package service

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func (s *Service) resolveSubjects(ctx context.Context, subject value.SubjectRef) ([]value.SubjectRef, string, error) {
	subjects := []value.SubjectRef{subject}
	if subject.Type != string(enum.AccessSubjectUser) && subject.Type != string(enum.AccessSubjectExternalAccount) {
		return subjects, "", nil
	}
	blocked, err := s.isBlockedAccessSubject(ctx, subject)
	if err != nil {
		return nil, "", err
	}
	if blocked {
		return subjects, reasonSubjectBlocked, nil
	}
	seen := map[string]struct{}{subject.Type + ":" + subject.ID: {}}
	queue := []value.SubjectRef{{Type: subject.Type, ID: subject.ID}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentMembershipType, ok := accessSubjectToMembershipSubject(current.Type)
		if !ok {
			continue
		}
		memberships, err := s.repository.ListMemberships(ctx, query.MembershipGraphFilter{
			Subject: value.SubjectRef{Type: string(currentMembershipType), ID: current.ID}, Status: enum.MembershipStatusActive,
		})
		if err != nil {
			return nil, "", err
		}
		for _, membership := range memberships {
			target := membershipTargetToAccessSubject(membership)
			if target.ID == "" {
				continue
			}
			subjects, queue = appendResolvedSubject(subjects, queue, seen, target)
			if target.Type == string(enum.AccessSubjectGroup) {
				subjects, queue, err = s.appendParentGroups(ctx, subjects, queue, seen, membership.TargetID)
				if err != nil {
					return nil, "", err
				}
			}
		}
	}
	return subjects, "", nil
}

func (s *Service) isBlockedAccessSubject(ctx context.Context, subject value.SubjectRef) (bool, error) {
	subjectID, err := uuid.Parse(subject.ID)
	if err != nil {
		return false, errs.ErrInvalidArgument
	}
	switch subject.Type {
	case string(enum.AccessSubjectUser):
		user, err := s.repository.GetUser(ctx, subjectID)
		if err != nil {
			return false, err
		}
		return user.Status == enum.UserStatusBlocked || user.Status == enum.UserStatusDisabled, nil
	case string(enum.AccessSubjectExternalAccount):
		account, err := s.repository.GetExternalAccount(ctx, subjectID)
		if err != nil {
			return false, err
		}
		return account.Status == enum.ExternalAccountStatusBlocked || account.Status == enum.ExternalAccountStatusDisabled, nil
	default:
		return false, nil
	}
}

func (s *Service) validateMembershipEndpoint(
	ctx context.Context,
	subjectType enum.MembershipSubjectType,
	subjectID uuid.UUID,
	targetType enum.MembershipTargetType,
	targetID uuid.UUID,
) error {
	if err := s.validateMembershipSubject(ctx, subjectType, subjectID); err != nil {
		return err
	}
	return s.validateMembershipTarget(ctx, targetType, targetID)
}

func (s *Service) validateMembershipSubject(ctx context.Context, subjectType enum.MembershipSubjectType, subjectID uuid.UUID) error {
	switch subjectType {
	case enum.MembershipSubjectUser:
		user, err := s.repository.GetUser(ctx, subjectID)
		if err != nil {
			return err
		}
		if user.Status == enum.UserStatusBlocked || user.Status == enum.UserStatusDisabled {
			return errs.ErrPreconditionFailed
		}
		return nil
	case enum.MembershipSubjectGroup:
		group, err := s.repository.GetGroup(ctx, subjectID)
		if err != nil {
			return err
		}
		if group.Status != enum.GroupStatusActive {
			return errs.ErrPreconditionFailed
		}
		return nil
	case enum.MembershipSubjectExternalAccount:
		account, err := s.repository.GetExternalAccount(ctx, subjectID)
		if err != nil {
			return err
		}
		if account.Status != enum.ExternalAccountStatusActive {
			return errs.ErrPreconditionFailed
		}
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func (s *Service) validateMembershipTarget(ctx context.Context, targetType enum.MembershipTargetType, targetID uuid.UUID) error {
	switch targetType {
	case enum.MembershipTargetOrganization:
		organization, err := s.repository.GetOrganization(ctx, targetID)
		if err != nil {
			return err
		}
		if organization.Status != enum.OrganizationStatusActive {
			return errs.ErrPreconditionFailed
		}
		return nil
	case enum.MembershipTargetGroup:
		group, err := s.repository.GetGroup(ctx, targetID)
		if err != nil {
			return err
		}
		if group.Status != enum.GroupStatusActive {
			return errs.ErrPreconditionFailed
		}
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func accessSubjectToMembershipSubject(subjectType string) (enum.MembershipSubjectType, bool) {
	switch subjectType {
	case string(enum.AccessSubjectUser):
		return enum.MembershipSubjectUser, true
	case string(enum.AccessSubjectGroup):
		return enum.MembershipSubjectGroup, true
	case string(enum.AccessSubjectExternalAccount):
		return enum.MembershipSubjectExternalAccount, true
	default:
		return "", false
	}
}

func membershipTargetToAccessSubject(membership entity.Membership) value.SubjectRef {
	switch membership.TargetType {
	case enum.MembershipTargetOrganization:
		return value.SubjectRef{Type: string(enum.AccessSubjectOrganization), ID: membership.TargetID.String()}
	case enum.MembershipTargetGroup:
		return value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: membership.TargetID.String()}
	default:
		return value.SubjectRef{}
	}
}

func appendResolvedSubject(
	subjects []value.SubjectRef,
	queue []value.SubjectRef,
	seen map[string]struct{},
	subject value.SubjectRef,
) ([]value.SubjectRef, []value.SubjectRef) {
	key := subject.Type + ":" + subject.ID
	if _, ok := seen[key]; ok {
		return subjects, queue
	}
	seen[key] = struct{}{}
	subjects = append(subjects, subject)
	if subject.Type == string(enum.AccessSubjectGroup) {
		queue = append(queue, subject)
	}
	return subjects, queue
}

func (s *Service) appendParentGroups(
	ctx context.Context,
	subjects []value.SubjectRef,
	queue []value.SubjectRef,
	seen map[string]struct{},
	groupID uuid.UUID,
) ([]value.SubjectRef, []value.SubjectRef, error) {
	for {
		group, err := s.repository.GetGroup(ctx, groupID)
		if err != nil {
			return nil, nil, err
		}
		if group.ParentGroupID == nil {
			return subjects, queue, nil
		}
		parent := value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: group.ParentGroupID.String()}
		if _, ok := seen[parent.Type+":"+parent.ID]; ok {
			return subjects, queue, nil
		}
		subjects, queue = appendResolvedSubject(subjects, queue, seen, parent)
		groupID = *group.ParentGroupID
	}
}

func sameUUIDPtr(a *uuid.UUID, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func sortedUnique(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func policyVersion(rules []entity.AccessRule) int64 {
	var version int64
	for _, rule := range rules {
		if rule.Version > version {
			version = rule.Version
		}
	}
	return version
}

func ruleExplanations(rules []entity.AccessRule, reasonCode string) []value.RuleExplanation {
	explanations := make([]value.RuleExplanation, 0, len(rules))
	for _, rule := range rules {
		explanations = append(explanations, value.RuleExplanation{
			RuleID: rule.ID, Effect: string(rule.Effect),
			Subject:   value.SubjectRef{Type: string(rule.SubjectType), ID: rule.SubjectID},
			ActionKey: rule.ActionKey, Scope: value.ScopeRef{Type: rule.ScopeType, ID: rule.ScopeID},
			Priority: rule.Priority, ReasonCode: reasonCode,
		})
	}
	return explanations
}
