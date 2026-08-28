package entity

import "time"

type PermissionDefinition struct {
	Key, NameKey, DescriptionKey string
	Risk                         string
	AllowedScopes, ResourceKinds []string
	OwnerConditionSupported      bool
}

type AccessSubject struct {
	Ref, Kind, DisplayName string
	Active                 bool
	OIDCGroupRefs          []string
}

type OIDCGroup struct {
	Ref, DisplayName, State    string
	MemberCount, BindingCount  int32
	LastSeenAt, SynchronizedAt time.Time
}

type AccessRoleVersion struct {
	Ref, RoleRef, Name, Description, ChangeComment string
	Revision                                       int64
	PermissionKeys, AllowedScopes                  []string
	CreatedAt                                      time.Time
	CreatedBy                                      User
}

type AccessRole struct {
	Ref, Kind, State string
	Version          int64
	CurrentVersion   AccessRoleVersion
	BindingCount     int32
	UpdatedAt        time.Time
}

type AccessScope struct {
	Kind, ProjectRef, ResourceKind, ResourceRef string
	RelatedResourceRefs                         map[string]string
}

type AccessConditions struct {
	ValidFrom, ValidUntil *time.Time
	RequireOwner          bool
}

type AccessBinding struct {
	Ref, State           string
	Version              int64
	Subject              AccessSubject
	RoleVersion          AccessRoleVersion
	Scope                AccessScope
	Conditions           AccessConditions
	CreatedAt, UpdatedAt time.Time
}

type AccessExplanationStep struct {
	Code, BindingRef, RoleRef, RoleVersionRef string
	SourceKind, SourceRef                     string
	Scope                                     AccessScope
}

type EffectiveAccessDecision struct {
	PermissionKey string
	Allowed       bool
	Target        AccessScope
	Explanation   []AccessExplanationStep
}

type EffectiveAccess struct {
	Subject     AccessSubject
	Decisions   []EffectiveAccessDecision
	EvaluatedAt time.Time
}

type AccessSimulation struct {
	Subject            AccessSubject
	Current, Simulated EffectiveAccessDecision
	EvaluatedAt        time.Time
}
