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

// ListMembershipGraph returns operator-visible membership edges around a subject.
func (s *Service) ListMembershipGraph(ctx context.Context, input ListMembershipGraphInput) (ListMembershipGraphResult, error) {
	subject, scopes, err := s.normalizeMembershipGraphSubject(ctx, input.Subject)
	if err != nil {
		return ListMembershipGraphResult{}, err
	}
	if err := s.requireAllowedInAnyScope(ctx, input.Meta, accessActionListMembershipGraph, value.ResourceRef{
		Type: accessResourceMembershipGraph,
		ID:   subject.Type + ":" + subject.ID,
	}, scopes); err != nil {
		return ListMembershipGraphResult{}, err
	}
	edges, err := s.collectMembershipGraphEdges(ctx, subject, input.IncludeInactive)
	if err != nil {
		return ListMembershipGraphResult{}, err
	}
	return ListMembershipGraphResult{Root: subject, Edges: edges}, nil
}

func (s *Service) resolveSubjects(ctx context.Context, subject value.SubjectRef) ([]value.SubjectRef, string, error) {
	subjects := []value.SubjectRef{subject}
	reasonCode, err := s.accessSubjectStopReason(ctx, subject)
	if err != nil {
		return nil, "", err
	}
	if reasonCode != "" {
		return subjects, reasonCode, nil
	}
	if _, ok := accessSubjectToMembershipSubject(subject.Type); !ok {
		return subjects, "", nil
	}
	lookup := newMembershipGraphLookup(s)
	seen := map[string]struct{}{subject.Type + ":" + subject.ID: {}}
	queue := []value.SubjectRef{{Type: subject.Type, ID: subject.ID}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentMembershipType, ok := accessSubjectToMembershipSubject(current.Type)
		if !ok {
			continue
		}
		if current.Type == string(enum.AccessSubjectGroup) {
			currentGroupID, err := uuid.Parse(current.ID)
			if err != nil {
				return nil, "", errs.ErrInvalidArgument
			}
			subjects, queue, err = lookup.appendParentGroups(ctx, subjects, queue, seen, currentGroupID)
			if err != nil {
				return nil, "", err
			}
		}
		memberships, err := s.repository.ListMemberships(ctx, query.MembershipGraphFilter{
			Subject:  value.SubjectRef{Type: string(currentMembershipType), ID: current.ID},
			Statuses: []enum.MembershipStatus{enum.MembershipStatusActive},
		})
		if err != nil {
			return nil, "", err
		}
		for _, membership := range memberships {
			target, effective, err := lookup.effectiveMembershipTarget(ctx, membership)
			if err != nil {
				return nil, "", err
			}
			if !effective {
				continue
			}
			subjects, queue = appendResolvedSubject(subjects, queue, seen, target)
			if target.Type == string(enum.AccessSubjectGroup) {
				subjects, queue, err = lookup.appendParentGroups(ctx, subjects, queue, seen, membership.TargetID)
				if err != nil {
					return nil, "", err
				}
			}
		}
	}
	return subjects, "", nil
}

// collectMembershipGraphEdges walks subject-side effective memberships and target-side organization/group roots.
func (s *Service) collectMembershipGraphEdges(ctx context.Context, root value.SubjectRef, includeInactive bool) ([]entity.Membership, error) {
	return s.collectMembershipGraphEdgesWithLookup(ctx, root, includeInactive, newMembershipGraphLookup(s))
}

func (s *Service) collectMembershipGraphEdgesWithLookup(
	ctx context.Context,
	root value.SubjectRef,
	includeInactive bool,
	lookup *membershipGraphLookup,
) ([]entity.Membership, error) {
	seenEdges := make(map[uuid.UUID]struct{})
	seenSubjects := make(map[string]struct{})
	seenTargets := make(map[string]struct{})
	var subjectQueue []value.SubjectRef
	var targetQueue []value.SubjectRef
	subjectQueue = enqueueMembershipGraphRef(subjectQueue, seenSubjects, root, membershipGraphQueueSubject)
	targetQueue = enqueueMembershipGraphRef(targetQueue, seenTargets, root, membershipGraphQueueTarget)
	var edges []entity.Membership
	for len(subjectQueue) > 0 || len(targetQueue) > 0 {
		for len(subjectQueue) > 0 {
			current := subjectQueue[0]
			subjectQueue = subjectQueue[1:]
			memberships, err := s.listMembershipsForGraph(ctx, current, includeInactive, membershipGraphDirectionSubject)
			if err != nil {
				return nil, err
			}
			for _, membership := range memberships {
				edges = appendMembershipGraphEdge(edges, seenEdges, membership)
				if membership.Status != enum.MembershipStatusActive {
					continue
				}
				target, effective, err := lookup.effectiveMembershipTarget(ctx, membership)
				if err != nil {
					return nil, err
				}
				if effective && target.Type == string(enum.AccessSubjectGroup) {
					subjectQueue = enqueueMembershipGraphRef(subjectQueue, seenSubjects, target, membershipGraphQueueSubject)
				}
			}
			if current.Type == string(enum.AccessSubjectGroup) {
				groupID, err := uuid.Parse(current.ID)
				if err != nil {
					return nil, errs.ErrInvalidArgument
				}
				parents, err := lookup.parentGroupSubjects(ctx, groupID)
				if err != nil {
					return nil, err
				}
				for _, parent := range parents {
					subjectQueue = enqueueMembershipGraphRef(subjectQueue, seenSubjects, parent, membershipGraphQueueSubject)
				}
			}
		}
		for len(targetQueue) > 0 {
			current := targetQueue[0]
			targetQueue = targetQueue[1:]
			memberships, err := s.listMembershipsForGraph(ctx, current, includeInactive, membershipGraphDirectionTarget)
			if err != nil {
				return nil, err
			}
			for _, membership := range memberships {
				edges = appendMembershipGraphEdge(edges, seenEdges, membership)
				if membership.Status != enum.MembershipStatusActive || membership.SubjectType != enum.MembershipSubjectGroup {
					continue
				}
				subject := value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: membership.SubjectID.String()}
				subjectQueue = enqueueMembershipGraphRef(subjectQueue, seenSubjects, subject, membershipGraphQueueSubject)
				targetQueue = enqueueMembershipGraphRef(targetQueue, seenTargets, subject, membershipGraphQueueTarget)
			}
		}
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return membershipGraphSortKey(edges[i]) < membershipGraphSortKey(edges[j])
	})
	return edges, nil
}

func (s *Service) listMembershipsForGraph(
	ctx context.Context,
	ref value.SubjectRef,
	includeInactive bool,
	direction membershipGraphDirection,
) ([]entity.Membership, error) {
	statuses := membershipGraphStatuses(includeInactive)
	switch direction {
	case membershipGraphDirectionSubject:
		return s.repository.ListMemberships(ctx, query.MembershipGraphFilter{Subject: ref, Statuses: statuses})
	case membershipGraphDirectionTarget:
		return s.repository.ListMembershipsByTarget(ctx, query.MembershipTargetFilter{Target: ref, Statuses: statuses})
	default:
		return nil, errs.ErrInvalidArgument
	}
}

type membershipGraphDirection string

const (
	membershipGraphDirectionSubject membershipGraphDirection = "subject"
	membershipGraphDirectionTarget  membershipGraphDirection = "target"
)

func membershipGraphStatuses(includeInactive bool) []enum.MembershipStatus {
	if !includeInactive {
		return []enum.MembershipStatus{enum.MembershipStatusActive}
	}
	return []enum.MembershipStatus{
		enum.MembershipStatusActive,
		enum.MembershipStatusPending,
		enum.MembershipStatusBlocked,
		enum.MembershipStatusDisabled,
	}
}

func enqueueMembershipGraphRef(queue []value.SubjectRef, seen map[string]struct{}, subject value.SubjectRef, kind membershipGraphQueueKind) []value.SubjectRef {
	if !membershipGraphQueueSupports(kind, subject.Type) {
		return queue
	}
	key := subject.Type + ":" + subject.ID
	if _, ok := seen[key]; ok {
		return queue
	}
	seen[key] = struct{}{}
	return append(queue, subject)
}

func membershipGraphQueueSupports(kind membershipGraphQueueKind, subjectType string) bool {
	switch kind {
	case membershipGraphQueueSubject:
		_, ok := accessSubjectToMembershipSubject(subjectType)
		return ok
	case membershipGraphQueueTarget:
		_, ok := accessSubjectToMembershipTarget(subjectType)
		return ok
	default:
		return false
	}
}

type membershipGraphQueueKind string

const (
	membershipGraphQueueSubject membershipGraphQueueKind = "subject"
	membershipGraphQueueTarget  membershipGraphQueueKind = "target"
)

func appendMembershipGraphEdge(edges []entity.Membership, seen map[uuid.UUID]struct{}, membership entity.Membership) []entity.Membership {
	if _, ok := seen[membership.ID]; ok {
		return edges
	}
	seen[membership.ID] = struct{}{}
	return append(edges, membership)
}

func (s *Service) accessSubjectStopReason(ctx context.Context, subject value.SubjectRef) (string, error) {
	subjectID, parsed, err := parseStoredAccessSubjectID(subject)
	if err != nil || !parsed {
		return "", err
	}
	switch subject.Type {
	case string(enum.AccessSubjectUser):
		user, err := s.repository.GetUser(ctx, subjectID)
		if err != nil {
			return "", err
		}
		if user.Status == enum.UserStatusPending {
			return reasonSubjectPending, nil
		}
		if user.Status == enum.UserStatusBlocked || user.Status == enum.UserStatusDisabled {
			return reasonSubjectBlocked, nil
		}
		return "", nil
	case string(enum.AccessSubjectExternalAccount):
		account, err := s.repository.GetExternalAccount(ctx, subjectID)
		if err != nil {
			return "", err
		}
		if account.Status != enum.ExternalAccountStatusActive {
			return reasonSubjectBlocked, nil
		}
		return "", nil
	case string(enum.AccessSubjectGroup):
		group, err := s.repository.GetGroup(ctx, subjectID)
		if err != nil {
			return "", err
		}
		if group.Status != enum.GroupStatusActive {
			return reasonSubjectBlocked, nil
		}
		return "", nil
	case string(enum.AccessSubjectOrganization):
		organization, err := s.repository.GetOrganization(ctx, subjectID)
		if err != nil {
			return "", err
		}
		if organization.Status != enum.OrganizationStatusActive {
			return reasonSubjectBlocked, nil
		}
		return "", nil
	default:
		return "", nil
	}
}

func parseStoredAccessSubjectID(subject value.SubjectRef) (uuid.UUID, bool, error) {
	switch subject.Type {
	case string(enum.AccessSubjectUser),
		string(enum.AccessSubjectExternalAccount),
		string(enum.AccessSubjectGroup),
		string(enum.AccessSubjectOrganization):
		subjectID, err := uuid.Parse(subject.ID)
		if err != nil {
			return uuid.Nil, false, errs.ErrInvalidArgument
		}
		return subjectID, true, nil
	default:
		return uuid.Nil, false, nil
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

func accessSubjectToMembershipTarget(subjectType string) (enum.MembershipTargetType, bool) {
	switch subjectType {
	case string(enum.AccessSubjectOrganization):
		return enum.MembershipTargetOrganization, true
	case string(enum.AccessSubjectGroup):
		return enum.MembershipTargetGroup, true
	default:
		return "", false
	}
}

type membershipGraphLookup struct {
	service       *Service
	groups        map[uuid.UUID]entity.Group
	organizations map[uuid.UUID]entity.Organization
}

func newMembershipGraphLookup(service *Service) *membershipGraphLookup {
	return &membershipGraphLookup{
		service:       service,
		groups:        make(map[uuid.UUID]entity.Group),
		organizations: make(map[uuid.UUID]entity.Organization),
	}
}

func (l *membershipGraphLookup) getGroup(ctx context.Context, id uuid.UUID) (entity.Group, error) {
	return lookupMembershipGraphCached(ctx, id, l.groups, l.service.repository.GetGroup)
}

func (l *membershipGraphLookup) getOrganization(ctx context.Context, id uuid.UUID) (entity.Organization, error) {
	return lookupMembershipGraphCached(ctx, id, l.organizations, l.service.repository.GetOrganization)
}

func lookupMembershipGraphCached[T any](ctx context.Context, id uuid.UUID, cache map[uuid.UUID]T, load func(context.Context, uuid.UUID) (T, error)) (T, error) {
	if item, ok := cache[id]; ok {
		return item, nil
	}
	item, err := load(ctx, id)
	if err != nil {
		var zero T
		return zero, err
	}
	cache[id] = item
	return item, nil
}

func (l *membershipGraphLookup) effectiveMembershipTarget(ctx context.Context, membership entity.Membership) (value.SubjectRef, bool, error) {
	switch membership.TargetType {
	case enum.MembershipTargetOrganization:
		organization, err := l.getOrganization(ctx, membership.TargetID)
		if err != nil {
			return value.SubjectRef{}, false, err
		}
		return effectiveSubjectRef(string(enum.AccessSubjectOrganization), organization.ID, organization.Status == enum.OrganizationStatusActive)
	case enum.MembershipTargetGroup:
		group, err := l.getGroup(ctx, membership.TargetID)
		if err != nil {
			return value.SubjectRef{}, false, err
		}
		return effectiveSubjectRef(string(enum.AccessSubjectGroup), group.ID, group.Status == enum.GroupStatusActive)
	default:
		return value.SubjectRef{}, false, nil
	}
}

func effectiveSubjectRef(subjectType string, id uuid.UUID, active bool) (value.SubjectRef, bool, error) {
	if !active {
		return value.SubjectRef{}, false, nil
	}
	return value.SubjectRef{Type: subjectType, ID: id.String()}, true, nil
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

func (l *membershipGraphLookup) appendParentGroups(
	ctx context.Context,
	subjects []value.SubjectRef,
	queue []value.SubjectRef,
	seen map[string]struct{},
	groupID uuid.UUID,
) ([]value.SubjectRef, []value.SubjectRef, error) {
	for {
		group, err := l.getGroup(ctx, groupID)
		if err != nil {
			return nil, nil, err
		}
		if group.Status != enum.GroupStatusActive {
			return subjects, queue, nil
		}
		if group.ParentGroupID == nil {
			return subjects, queue, nil
		}
		parentGroup, err := l.getGroup(ctx, *group.ParentGroupID)
		if err != nil {
			return nil, nil, err
		}
		if parentGroup.Status != enum.GroupStatusActive {
			return subjects, queue, nil
		}
		parent := value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: parentGroup.ID.String()}
		if _, ok := seen[parent.Type+":"+parent.ID]; ok {
			return subjects, queue, nil
		}
		subjects, queue = appendResolvedSubject(subjects, queue, seen, parent)
		groupID = parentGroup.ID
	}
}

func (l *membershipGraphLookup) parentGroupSubjects(ctx context.Context, groupID uuid.UUID) ([]value.SubjectRef, error) {
	var parents []value.SubjectRef
	seen := make(map[uuid.UUID]struct{})
	for {
		if _, ok := seen[groupID]; ok {
			return parents, nil
		}
		seen[groupID] = struct{}{}
		group, err := l.getGroup(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if group.Status != enum.GroupStatusActive || group.ParentGroupID == nil {
			return parents, nil
		}
		parentGroup, err := l.getGroup(ctx, *group.ParentGroupID)
		if err != nil {
			return nil, err
		}
		if parentGroup.Status != enum.GroupStatusActive {
			return parents, nil
		}
		parents = append(parents, value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: parentGroup.ID.String()})
		groupID = parentGroup.ID
	}
}

func (s *Service) normalizeMembershipGraphSubject(ctx context.Context, subject value.SubjectRef) (value.SubjectRef, []value.ScopeRef, error) {
	subject.Type = strings.TrimSpace(subject.Type)
	subject.ID = strings.TrimSpace(subject.ID)
	if subject.Type == "" || subject.ID == "" {
		return value.SubjectRef{}, nil, errs.ErrInvalidArgument
	}
	if _, _, err := parseStoredAccessSubjectID(subject); err != nil {
		return value.SubjectRef{}, nil, err
	}
	if _, err := s.accessSubjectStopReason(ctx, subject); err != nil {
		return value.SubjectRef{}, nil, err
	}
	scopes, err := s.membershipGraphAccessScopes(ctx, subject)
	if err != nil {
		return value.SubjectRef{}, nil, err
	}
	return subject, scopes, nil
}

func (s *Service) membershipGraphAccessScopes(ctx context.Context, subject value.SubjectRef) ([]value.ScopeRef, error) {
	subjectID, _, err := parseStoredAccessSubjectID(subject)
	if err != nil {
		return nil, err
	}
	switch subject.Type {
	case string(enum.AccessSubjectUser):
		scopes, err := s.repository.ListUserAccessScopes(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		graphScopes, err := s.membershipGraphScopesFromEdges(ctx, subject)
		if err != nil {
			return nil, err
		}
		return append(scopes, graphScopes...), nil
	case string(enum.AccessSubjectGroup):
		group, err := s.repository.GetGroup(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		var scopes []value.ScopeRef
		if group.ScopeType == enum.GroupScopeOrganization && group.ScopeID != nil {
			scopes = append(scopes, value.ScopeRef{Type: accessRuleScopeOrganization, ID: group.ScopeID.String()})
		} else {
			scopes = append(scopes, value.ScopeRef{Type: accessRuleScopeGlobal})
		}
		graphScopes, err := s.membershipGraphScopesFromEdges(ctx, subject)
		if err != nil {
			return nil, err
		}
		return append(scopes, graphScopes...), nil
	case string(enum.AccessSubjectOrganization):
		return []value.ScopeRef{{Type: accessRuleScopeOrganization, ID: subject.ID}}, nil
	case string(enum.AccessSubjectExternalAccount):
		account, err := s.repository.GetExternalAccount(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		return []value.ScopeRef{externalAccountOwnerScope(account)}, nil
	default:
		return nil, errs.ErrInvalidArgument
	}
}

func (s *Service) membershipGraphScopesFromEdges(ctx context.Context, subject value.SubjectRef) ([]value.ScopeRef, error) {
	// Operators must authorize pending/blocked edges without expanding traversal through them.
	lookup := newMembershipGraphLookup(s)
	edges, err := s.collectMembershipGraphEdgesWithLookup(ctx, subject, true, lookup)
	if err != nil {
		return nil, err
	}
	var scopes []value.ScopeRef
	for _, edge := range edges {
		if !membershipGraphScopeStatus(edge.Status) {
			continue
		}
		switch edge.TargetType {
		case enum.MembershipTargetOrganization:
			scopes = append(scopes, value.ScopeRef{Type: accessRuleScopeOrganization, ID: edge.TargetID.String()})
		case enum.MembershipTargetGroup:
			group, err := lookup.getGroup(ctx, edge.TargetID)
			if err != nil {
				return nil, err
			}
			if group.ScopeType == enum.GroupScopeOrganization && group.ScopeID != nil {
				scopes = append(scopes, value.ScopeRef{Type: accessRuleScopeOrganization, ID: group.ScopeID.String()})
			}
		}
	}
	return scopes, nil
}

func membershipGraphScopeStatus(status enum.MembershipStatus) bool {
	switch status {
	case enum.MembershipStatusActive, enum.MembershipStatusPending, enum.MembershipStatusBlocked:
		return true
	default:
		return false
	}
}

func membershipGraphSortKey(edge entity.Membership) string {
	return string(edge.SubjectType) + ":" + edge.SubjectID.String() + ">" +
		string(edge.TargetType) + ":" + edge.TargetID.String() + ":" +
		string(edge.Status) + ":" + edge.ID.String()
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
