package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

type MembershipGraphFilter struct {
	Subject value.SubjectRef
	Status  enum.MembershipStatus
}

type MembershipIdentity struct {
	SubjectType enum.MembershipSubjectType
	SubjectID   uuid.UUID
	TargetType  enum.MembershipTargetType
	TargetID    uuid.UUID
}

type AccessRuleFilter struct {
	Subjects     []value.SubjectRef
	ActionKey    string
	ResourceType string
	ResourceID   string
	Scope        value.ScopeRef
}

type ExternalAccountUsageFilter struct {
	ExternalAccountID uuid.UUID
	ActionKey         string
	UsageScope        value.ScopeRef
}
