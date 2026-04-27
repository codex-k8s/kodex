// Package query contains typed repository filters for access-manager reads.
package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

// MembershipGraphFilter selects active memberships for subject graph expansion.
type MembershipGraphFilter struct {
	Subject value.SubjectRef
	Status  enum.MembershipStatus
}

// MembershipIdentity is the natural key of a membership edge.
type MembershipIdentity struct {
	SubjectType enum.MembershipSubjectType
	SubjectID   uuid.UUID
	TargetType  enum.MembershipTargetType
	TargetID    uuid.UUID
}

// AccessRuleFilter selects policy rules applicable to an access check.
type AccessRuleFilter struct {
	Subjects     []value.SubjectRef
	ActionKey    string
	ResourceType string
	ResourceID   string
	Scope        value.ScopeRef
}

// ExternalAccountUsageFilter selects an account binding for a requested usage.
type ExternalAccountUsageFilter struct {
	ExternalAccountID uuid.UUID
	ActionKey         string
	UsageScope        value.ScopeRef
}
