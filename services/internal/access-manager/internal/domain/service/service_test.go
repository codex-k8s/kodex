package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func TestBootstrapUserFromIdentityUsesAllowlistDomain(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	org, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex", DisplayName: "Платформа KODEX",
		Meta: commandMeta("create-owner-org"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	_, err = svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchDomain, Value: "Example.com", OrganizationID: &org.ID, DefaultStatus: enum.UserStatusActive,
	})
	if err != nil {
		t.Fatalf("put allowlist: %v", err)
	}

	result, err := svc.BootstrapUserFromIdentity(ctx, BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderGitHub, Subject: "42", Email: "Owner@Example.com", DisplayName: "Owner",
	})
	if err != nil {
		t.Fatalf("bootstrap user: %v", err)
	}
	if result.Decision != enum.AccessDecisionAllow {
		t.Fatalf("decision = %s, want %s", result.Decision, enum.AccessDecisionAllow)
	}
	if result.User.PrimaryEmail != "owner@example.com" {
		t.Fatalf("email = %s", result.User.PrimaryEmail)
	}
	if result.Organization == nil || result.Organization.ID != org.ID {
		t.Fatalf("organization was not resolved")
	}
}

func TestCreateOrganizationTreatsEmptyStatusAsActiveForOwnerGuard(t *testing.T) {
	ctx := context.Background()
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())

	_, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex", DisplayName: "Платформа KODEX",
		Meta: commandMeta("create-owner-org"),
	})
	if err != nil {
		t.Fatalf("create first owner: %v", err)
	}
	_, err = svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex-2", DisplayName: "Платформа KODEX 2",
		Meta: commandMeta("create-second-owner-org"),
	})
	if !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("err = %v, want %v", err, errs.ErrAlreadyExists)
	}
}

func TestCreateOrganizationRejectsNonActiveOwner(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.CreateOrganization(context.Background(), CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex", DisplayName: "Платформа KODEX",
		Status: enum.OrganizationStatusPending, Meta: commandMeta("create-pending-owner"),
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestCreateOrganizationRequiresCommandIdentity(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.CreateOrganization(context.Background(), CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex", DisplayName: "Платформа KODEX",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestCreateOrganizationReplaysCommandResult(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	created, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "kodex", DisplayName: "Платформа KODEX",
		Meta: commandMeta("create-owner-idempotent"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	replayed, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindOwner, Slug: "different", DisplayName: "Другой владелец",
		Meta: commandMeta("create-owner-idempotent"),
	})
	if err != nil {
		t.Fatalf("replay organization command: %v", err)
	}
	if replayed.ID != created.ID || replayed.Version != created.Version {
		t.Fatalf("replay changed result: id %s/%s version %d/%d", replayed.ID, created.ID, replayed.Version, created.Version)
	}
	if len(store.organizations) != 1 || len(store.events) != 1 {
		t.Fatalf("stored organizations/events = %d/%d, want 1/1", len(store.organizations), len(store.events))
	}
}

func TestBootstrapUserFromIdentityResolvesOrganizationBeforeCreateUser(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	missingOrganizationID := uuid.New()

	_, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchDomain, Value: "example.com", OrganizationID: &missingOrganizationID,
		DefaultStatus: enum.UserStatusActive,
	})
	if err != nil {
		t.Fatalf("put allowlist: %v", err)
	}
	_, err = svc.BootstrapUserFromIdentity(ctx, BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderGitHub, Subject: "42", Email: "owner@example.com",
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want %v", err, errs.ErrNotFound)
	}
	if len(store.users) != 0 {
		t.Fatalf("users were created after failed organization lookup")
	}
}

func TestBootstrapUserFromIdentityRejectsDisabledAllowlist(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	_, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchEmail, Value: "owner@example.com", DefaultStatus: enum.UserStatusActive,
		Status: enum.AllowlistStatusDisabled,
	})
	if err != nil {
		t.Fatalf("put allowlist: %v", err)
	}
	_, err = svc.BootstrapUserFromIdentity(ctx, BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderGitHub, Subject: "42", Email: "owner@example.com",
	})
	if !errors.Is(err, errs.ErrUnauthorizedSubject) {
		t.Fatalf("err = %v, want %v", err, errs.ErrUnauthorizedSubject)
	}
	if len(store.users) != 0 {
		t.Fatalf("users were created from disabled allowlist")
	}
}

func TestBootstrapUserFromIdentityLinksExistingUserByEmail(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	_, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchDomain, Value: "example.com", DefaultStatus: enum.UserStatusActive,
	})
	if err != nil {
		t.Fatalf("put allowlist: %v", err)
	}
	created, err := svc.BootstrapUserFromIdentity(ctx, BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderGitHub, Subject: "42", Email: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("bootstrap first identity: %v", err)
	}
	linked, err := svc.BootstrapUserFromIdentity(ctx, BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderKeycloak, Subject: "kc-42", Email: "OWNER@example.com",
	})
	if err != nil {
		t.Fatalf("bootstrap second identity: %v", err)
	}
	if linked.User.ID != created.User.ID {
		t.Fatalf("linked user = %s, want %s", linked.User.ID, created.User.ID)
	}
	if linked.ReasonCode != reasonIdentityLinked {
		t.Fatalf("reason = %s, want %s", linked.ReasonCode, reasonIdentityLinked)
	}
	if len(store.users) != 1 {
		t.Fatalf("user count = %d, want 1", len(store.users))
	}
}

func TestPutAllowlistEntryRejectsBlockedDefaultStatus(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.PutAllowlistEntry(context.Background(), PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchEmail, Value: "owner@example.com", DefaultStatus: enum.UserStatusBlocked,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestPutAllowlistEntryKeepsIdentityOnRepeat(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	created, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchEmail, Value: "Owner@Example.com", DefaultStatus: enum.UserStatusActive,
	})
	if err != nil {
		t.Fatalf("create allowlist: %v", err)
	}
	repeated, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchEmail, Value: "owner@example.com", DefaultStatus: enum.UserStatusActive,
	})
	if err != nil {
		t.Fatalf("repeat allowlist: %v", err)
	}
	if repeated.ID != created.ID || repeated.Version != created.Version {
		t.Fatalf("repeat changed identity/version: id %s/%s version %d/%d", repeated.ID, created.ID, repeated.Version, created.Version)
	}
	updated, err := svc.PutAllowlistEntry(ctx, PutAllowlistEntryInput{
		MatchType: enum.AllowlistMatchEmail, Value: "owner@example.com", DefaultStatus: enum.UserStatusPending,
		Meta: value.CommandMeta{ExpectedVersion: ptrInt64(created.Version)},
	})
	if err != nil {
		t.Fatalf("update allowlist: %v", err)
	}
	if updated.ID != created.ID || updated.Version != created.Version+1 {
		t.Fatalf("update identity/version = %s/%d, want %s/%d", updated.ID, updated.Version, created.ID, created.Version+1)
	}
}

func TestCheckAccessExplicitDenyWins(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	user := store.seedUser(enum.UserStatusActive)
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "project", ScopeID: "project-1",
		Priority: 10,
	})
	if err != nil {
		t.Fatalf("put allow rule: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectDeny, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "project", ScopeID: "project-1",
		Priority: 1,
	})
	if err != nil {
		t.Fatalf("put deny rule: %v", err)
	}

	result, err := svc.CheckAccess(ctx, CheckAccessInput{
		Subject:   value.SubjectRef{Type: string(enum.AccessSubjectUser), ID: user.ID.String()},
		ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
		Scope: value.ScopeRef{Type: "project", ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if result.Decision != enum.AccessDecisionDeny {
		t.Fatalf("decision = %s, want %s", result.Decision, enum.AccessDecisionDeny)
	}
	if result.ReasonCode != reasonExplicitDeny {
		t.Fatalf("reason = %s, want %s", result.ReasonCode, reasonExplicitDeny)
	}
	if len(store.audits) != 1 {
		t.Fatalf("audit count = %d, want 1", len(store.audits))
	}
}

func TestPutExternalProviderCreateOnlyRejectsMutation(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	_, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub", CreateOnly: true,
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	_, err = svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub Enterprise", CreateOnly: true,
	})
	if !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("err = %v, want %v", err, errs.ErrAlreadyExists)
	}
}

func TestCheckAccessRejectsBlankRequiredInput(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.CheckAccess(context.Background(), CheckAccessInput{
		Subject:  value.SubjectRef{Type: string(enum.AccessSubjectUser), ID: uuid.New().String()},
		Resource: value.ResourceRef{Type: "project", ID: "project-1"},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}

	_, err = svc.CheckAccess(context.Background(), CheckAccessInput{
		ActionKey: "project.read",
		Resource:  value.ResourceRef{Type: "project", ID: "project-1"},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("subject err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestCheckAccessResolvesTransitiveMembershipGraph(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	user := store.seedUser(enum.UserStatusActive)
	org, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindClient, Slug: "client", DisplayName: "Клиент",
		Meta: commandMeta("create-client-org"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	group, err := svc.CreateGroup(ctx, CreateGroupInput{
		ScopeType: enum.GroupScopeOrganization, ScopeID: &org.ID, Slug: "dev", DisplayName: "Разработчики",
		Meta: commandMeta("create-dev-group"),
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: group.ID,
	})
	if err != nil {
		t.Fatalf("set user group membership: %v", err)
	}
	_, err = svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectGroup, SubjectID: group.ID,
		TargetType: enum.MembershipTargetOrganization, TargetID: org.ID,
	})
	if err != nil {
		t.Fatalf("set group organization membership: %v", err)
	}
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectOrganization, SubjectID: org.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "project", ScopeID: "project-1",
	})
	if err != nil {
		t.Fatalf("put rule: %v", err)
	}

	result, err := svc.CheckAccess(ctx, CheckAccessInput{
		Subject:   value.SubjectRef{Type: string(enum.AccessSubjectUser), ID: user.ID.String()},
		ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
		Scope: value.ScopeRef{Type: "project", ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if result.Decision != enum.AccessDecisionAllow {
		t.Fatalf("decision = %s, want %s", result.Decision, enum.AccessDecisionAllow)
	}
}

func TestCheckAccessDeniesNonActiveExternalAccountSubject(t *testing.T) {
	statuses := []enum.ExternalAccountStatus{
		enum.ExternalAccountStatusPending,
		enum.ExternalAccountStatusNeedsReauth,
		enum.ExternalAccountStatusLimited,
		enum.ExternalAccountStatusBlocked,
		enum.ExternalAccountStatusDisabled,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			store := newMemoryRepository()
			svc := New(store, fixedClock{}, newSequenceIDs())

			provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
				Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
			})
			if err != nil {
				t.Fatalf("put provider: %v", err)
			}
			account, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
				ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "bot",
				Status: status, Meta: commandMeta("register-account-" + string(status)),
			})
			if err != nil {
				t.Fatalf("register account: %v", err)
			}
			action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
				Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
			})
			if err != nil {
				t.Fatalf("put action: %v", err)
			}
			_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
				Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectExternalAccount, SubjectID: account.ID.String(),
				ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "global",
			})
			if err != nil {
				t.Fatalf("put rule: %v", err)
			}

			result, err := svc.CheckAccess(ctx, CheckAccessInput{
				Subject:   value.SubjectRef{Type: string(enum.AccessSubjectExternalAccount), ID: account.ID.String()},
				ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
				Scope: value.ScopeRef{Type: "project", ID: "project-1"},
			})
			if err != nil {
				t.Fatalf("check access: %v", err)
			}
			if result.Decision != enum.AccessDecisionDeny || result.ReasonCode != reasonSubjectBlocked {
				t.Fatalf("decision/reason = %s/%s, want %s/%s", result.Decision, result.ReasonCode, enum.AccessDecisionDeny, reasonSubjectBlocked)
			}
		})
	}
}

func TestCheckAccessSkipsInactiveMembershipTarget(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	user := store.seedUser(enum.UserStatusActive)
	org, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindClient, Slug: "client", DisplayName: "Клиент",
		Meta: commandMeta("create-client-org-for-inactive-target"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	group, err := svc.CreateGroup(ctx, CreateGroupInput{
		ScopeType: enum.GroupScopeOrganization, ScopeID: &org.ID, Slug: "ops", DisplayName: "Операторы",
		Meta: commandMeta("create-ops-group"),
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	_, err = svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: group.ID,
	})
	if err != nil {
		t.Fatalf("set membership: %v", err)
	}
	group.Status = enum.GroupStatusDisabled
	store.groups[group.ID] = group

	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectGroup, SubjectID: group.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "global",
	})
	if err != nil {
		t.Fatalf("put rule: %v", err)
	}

	result, err := svc.CheckAccess(ctx, CheckAccessInput{
		Subject:   value.SubjectRef{Type: string(enum.AccessSubjectUser), ID: user.ID.String()},
		ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
		Scope: value.ScopeRef{Type: "project", ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if result.Decision != enum.AccessDecisionDeny || result.ReasonCode != reasonNoMatchingRule {
		t.Fatalf("decision/reason = %s/%s, want %s/%s", result.Decision, result.ReasonCode, enum.AccessDecisionDeny, reasonNoMatchingRule)
	}
}

func TestCheckAccessResolvesRootGroupParents(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	org, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindClient, Slug: "client", DisplayName: "Клиент",
		Meta: commandMeta("create-client-org-for-root-groups"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	parent, err := svc.CreateGroup(ctx, CreateGroupInput{
		ScopeType: enum.GroupScopeOrganization, ScopeID: &org.ID, Slug: "owners", DisplayName: "Владельцы",
		Meta: commandMeta("create-owners-group"),
	})
	if err != nil {
		t.Fatalf("create parent group: %v", err)
	}
	child, err := svc.CreateGroup(ctx, CreateGroupInput{
		ScopeType: enum.GroupScopeOrganization, ScopeID: &org.ID, Slug: "reviewers", DisplayName: "Ревьюеры",
		ParentGroupID: &parent.ID, Meta: commandMeta("create-reviewers-group"),
	})
	if err != nil {
		t.Fatalf("create child group: %v", err)
	}
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectGroup, SubjectID: parent.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ResourceID: "project-1", ScopeType: "global",
	})
	if err != nil {
		t.Fatalf("put rule: %v", err)
	}

	result, err := svc.CheckAccess(ctx, CheckAccessInput{
		Subject:   value.SubjectRef{Type: string(enum.AccessSubjectGroup), ID: child.ID.String()},
		ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
		Scope: value.ScopeRef{Type: "project", ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if result.Decision != enum.AccessDecisionAllow {
		t.Fatalf("decision = %s, want %s", result.Decision, enum.AccessDecisionAllow)
	}
}

func TestSetMembershipUpdatesExistingIdentityAndVersion(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)
	org, err := svc.CreateOrganization(ctx, CreateOrganizationInput{
		Kind: enum.OrganizationKindClient, Slug: "client", DisplayName: "Клиент",
		Meta: commandMeta("create-client-org-for-membership"),
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	group, err := svc.CreateGroup(ctx, CreateGroupInput{
		ScopeType: enum.GroupScopeOrganization, ScopeID: &org.ID, Slug: "qa", DisplayName: "QA",
		Meta: commandMeta("create-qa-group"),
	})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	created, err := svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: group.ID,
	})
	if err != nil {
		t.Fatalf("create membership: %v", err)
	}
	updated, err := svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: group.ID,
		RoleHint: "owner",
	})
	if err != nil {
		t.Fatalf("update membership: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("membership ID changed: %s != %s", updated.ID, created.ID)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, created.Version+1)
	}
	_, err = svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: group.ID,
		Meta: value.CommandMeta{
			ExpectedVersion: ptrInt64(created.Version),
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestSetMembershipRequiresExistingEndpoints(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)

	_, err := svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: user.ID,
		TargetType: enum.MembershipTargetGroup, TargetID: uuid.New(),
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("target err = %v, want %v", err, errs.ErrNotFound)
	}
	_, err = svc.SetMembership(ctx, SetMembershipInput{
		SubjectType: enum.MembershipSubjectUser, SubjectID: uuid.New(),
		TargetType: enum.MembershipTargetGroup, TargetID: uuid.New(),
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("subject err = %v, want %v", err, errs.ErrNotFound)
	}
}

func TestResolveExternalAccountUsageRequiresAllowedActionAndSecret(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())

	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	secret := store.seedSecret(enum.SecretStoreVault, "kv/kodex/github/bot")
	account, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "kodex-agent",
		OwnerScopeType: enum.ExternalAccountScopeGlobal, Status: enum.ExternalAccountStatusActive, SecretBindingRefID: &secret.ID,
		Meta: commandMeta("register-github-bot-with-secret"),
	})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	_, err = svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "provider.issue.write", DisplayName: "Запись Issue", ResourceType: "provider_issue",
	})
	if err != nil {
		t.Fatalf("put access action: %v", err)
	}
	_, err = svc.BindExternalAccount(ctx, BindExternalAccountInput{
		ExternalAccountID: account.ID, UsageScopeType: enum.ExternalAccountScopeProject, UsageScopeID: "project-1",
		AllowedActionKeys: []string{"provider.issue.write"}, Status: enum.ExternalAccountBindingStatusActive,
	})
	if err != nil {
		t.Fatalf("bind account: %v", err)
	}

	result, err := svc.ResolveExternalAccountUsage(ctx, ResolveExternalAccountUsageInput{
		ExternalAccountID: account.ID, ActionKey: "provider.issue.write",
		UsageScope: value.ScopeRef{Type: string(enum.ExternalAccountScopeProject), ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("resolve usage: %v", err)
	}
	if result.SecretRef.StoreRef != secret.StoreRef {
		t.Fatalf("secret ref = %s, want %s", result.SecretRef.StoreRef, secret.StoreRef)
	}
}

func TestBindExternalAccountRejectsBlankAllowedActions(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.BindExternalAccount(context.Background(), BindExternalAccountInput{
		ExternalAccountID: uuid.New(),
		UsageScopeType:    enum.ExternalAccountScopeProject,
		UsageScopeID:      "project-1",
		AllowedActionKeys: []string{
			"   ",
		},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestBindExternalAccountRequiresCatalogAction(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	account, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "kodex-agent",
		Meta: commandMeta("register-github-bot-for-binding-action"),
	})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	_, err = svc.BindExternalAccount(ctx, BindExternalAccountInput{
		ExternalAccountID: account.ID, UsageScopeType: enum.ExternalAccountScopeProject, UsageScopeID: "project-1",
		AllowedActionKeys: []string{"provider.issue.write"},
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v, want %v", err, errs.ErrNotFound)
	}
}

func TestBindExternalAccountKeepsIdentityOnUpdate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	account, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "kodex-agent",
		Meta: commandMeta("register-github-bot-for-binding-update"),
	})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	_, err = svc.PutAccessAction(ctx, PutAccessActionInput{Key: "provider.issue.write", DisplayName: "Запись Issue", ResourceType: "provider_issue"})
	if err != nil {
		t.Fatalf("put first action: %v", err)
	}
	_, err = svc.PutAccessAction(ctx, PutAccessActionInput{Key: "provider.pr.write", DisplayName: "Запись PR", ResourceType: "provider_pr"})
	if err != nil {
		t.Fatalf("put second action: %v", err)
	}
	created, err := svc.BindExternalAccount(ctx, BindExternalAccountInput{
		ExternalAccountID: account.ID, UsageScopeType: enum.ExternalAccountScopeProject, UsageScopeID: "project-1",
		AllowedActionKeys: []string{"provider.issue.write"},
	})
	if err != nil {
		t.Fatalf("bind account: %v", err)
	}
	updated, err := svc.BindExternalAccount(ctx, BindExternalAccountInput{
		ExternalAccountID: account.ID, UsageScopeType: enum.ExternalAccountScopeProject, UsageScopeID: "project-1",
		AllowedActionKeys: []string{"provider.issue.write", "provider.pr.write"},
		Meta:              value.CommandMeta{ExpectedVersion: ptrInt64(created.Version)},
	})
	if err != nil {
		t.Fatalf("update binding: %v", err)
	}
	if updated.ID != created.ID || updated.Version != created.Version+1 {
		t.Fatalf("update identity/version = %s/%d, want %s/%d", updated.ID, updated.Version, created.ID, created.Version+1)
	}
}

func TestBindExternalAccountRejectsDisabledStatus(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	account, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "kodex-agent",
		Meta: commandMeta("register-github-bot-for-disabled-binding"),
	})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	_, err = svc.PutAccessAction(ctx, PutAccessActionInput{Key: "provider.issue.write", DisplayName: "Запись Issue", ResourceType: "provider_issue"})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.BindExternalAccount(ctx, BindExternalAccountInput{
		ExternalAccountID: account.ID, UsageScopeType: enum.ExternalAccountScopeProject, UsageScopeID: "project-1",
		AllowedActionKeys: []string{"provider.issue.write"}, Status: enum.ExternalAccountBindingStatusDisabled,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRegisterExternalAccountReplaysCommandResult(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	created, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "kodex-agent",
		Meta: commandMeta("register-bot-idempotent"),
	})
	if err != nil {
		t.Fatalf("register account: %v", err)
	}
	replayed, err := svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountUser, DisplayName: "another-account",
		Meta: commandMeta("register-bot-idempotent"),
	})
	if err != nil {
		t.Fatalf("replay register account command: %v", err)
	}
	if replayed.ID != created.ID || replayed.Version != created.Version {
		t.Fatalf("replay changed account: id %s/%s version %d/%d", replayed.ID, created.ID, replayed.Version, created.Version)
	}
	if len(store.accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(store.accounts))
	}
}

func TestPutAccessRuleRequiresActiveCatalogAction(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)

	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project", Status: enum.AccessActionStatusDisabled,
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global",
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestRegisterExternalAccountRejectsInvalidOwnerScope(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	provider, err := svc.PutExternalProvider(ctx, PutExternalProviderInput{
		Slug: "github", ProviderKind: enum.ExternalProviderRepository, DisplayName: "GitHub",
	})
	if err != nil {
		t.Fatalf("put provider: %v", err)
	}
	_, err = svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "bad",
		OwnerScopeType: enum.ExternalAccountScopeStage, OwnerScopeID: "stage-1",
		Meta: commandMeta("register-invalid-stage-owner"),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	_, err = svc.RegisterExternalAccount(ctx, RegisterExternalAccountInput{
		ExternalProviderID: provider.ID, AccountType: enum.ExternalAccountBot, DisplayName: "bad",
		OwnerScopeType: enum.ExternalAccountScopeProject,
		Meta:           commandMeta("register-invalid-project-owner"),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("missing scope id err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestPutAccessRuleKeepsIdentityOnUpdate(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	created, err := svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global",
	})
	if err != nil {
		t.Fatalf("put rule: %v", err)
	}
	repeated, err := svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global",
	})
	if err != nil {
		t.Fatalf("repeat rule: %v", err)
	}
	if repeated.ID != created.ID || repeated.Version != created.Version {
		t.Fatalf("repeat changed identity/version: id %s/%s version %d/%d", repeated.ID, created.ID, repeated.Version, created.Version)
	}
	updated, err := svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global", Priority: 50,
		Meta: value.CommandMeta{ExpectedVersion: ptrInt64(created.Version)},
	})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.ID != created.ID || updated.Version != created.Version+1 {
		t.Fatalf("update identity/version = %s/%d, want %s/%d", updated.ID, updated.Version, created.ID, created.Version+1)
	}
}

func TestPutAccessRuleRejectsDisabledStatus(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global", Status: enum.AccessRuleStatusDisabled,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestCheckAccessUsesGlobalScopeRule(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRepository()
	svc := New(store, fixedClock{}, newSequenceIDs())
	user := store.seedUser(enum.UserStatusActive)
	action, err := svc.PutAccessAction(ctx, PutAccessActionInput{
		Key: "project.read", DisplayName: "Чтение проекта", ResourceType: "project",
	})
	if err != nil {
		t.Fatalf("put action: %v", err)
	}
	_, err = svc.PutAccessRule(ctx, PutAccessRuleInput{
		Effect: enum.AccessEffectAllow, SubjectType: enum.AccessSubjectUser, SubjectID: user.ID.String(),
		ActionKey: action.Key, ResourceType: "project", ScopeType: "global",
	})
	if err != nil {
		t.Fatalf("put global rule: %v", err)
	}

	result, err := svc.CheckAccess(ctx, CheckAccessInput{
		Subject:   value.SubjectRef{Type: string(enum.AccessSubjectUser), ID: user.ID.String()},
		ActionKey: action.Key, Resource: value.ResourceRef{Type: "project", ID: "project-1"},
		Scope: value.ScopeRef{Type: "project", ID: "project-1"},
	})
	if err != nil {
		t.Fatalf("check access: %v", err)
	}
	if result.Decision != enum.AccessDecisionAllow {
		t.Fatalf("decision = %s, want %s", result.Decision, enum.AccessDecisionAllow)
	}
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
}

type sequenceIDs struct {
	next int
}

func newSequenceIDs() *sequenceIDs {
	return &sequenceIDs{}
}

func (g *sequenceIDs) New() uuid.UUID {
	g.next++
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(time.Unix(int64(g.next), 0).String()))
}

type memoryRepository struct {
	organizations map[uuid.UUID]entity.Organization
	users         map[uuid.UUID]entity.User
	identities    map[string]entity.UserIdentity
	allowlist     map[string]entity.AllowlistEntry
	groups        map[uuid.UUID]entity.Group
	memberships   map[uuid.UUID]entity.Membership
	providers     map[uuid.UUID]entity.ExternalProvider
	accounts      map[uuid.UUID]entity.ExternalAccount
	bindings      map[uuid.UUID]entity.ExternalAccountBinding
	secrets       map[uuid.UUID]entity.SecretBindingRef
	actions       map[string]entity.AccessAction
	rules         map[uuid.UUID]entity.AccessRule
	commands      map[string]entity.CommandResult
	audits        []entity.AccessDecisionAudit
	events        []entity.OutboxEvent
	ids           *sequenceIDs
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		organizations: make(map[uuid.UUID]entity.Organization),
		users:         make(map[uuid.UUID]entity.User),
		identities:    make(map[string]entity.UserIdentity),
		allowlist:     make(map[string]entity.AllowlistEntry),
		groups:        make(map[uuid.UUID]entity.Group),
		memberships:   make(map[uuid.UUID]entity.Membership),
		providers:     make(map[uuid.UUID]entity.ExternalProvider),
		accounts:      make(map[uuid.UUID]entity.ExternalAccount),
		bindings:      make(map[uuid.UUID]entity.ExternalAccountBinding),
		secrets:       make(map[uuid.UUID]entity.SecretBindingRef),
		actions:       make(map[string]entity.AccessAction),
		rules:         make(map[uuid.UUID]entity.AccessRule),
		commands:      make(map[string]entity.CommandResult),
		ids:           newSequenceIDs(),
	}
}

func (r *memoryRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	for _, result := range r.commands {
		if identity.CommandID != uuid.Nil && result.CommandID == identity.CommandID {
			return result, nil
		}
	}
	if identity.CommandID != uuid.Nil {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	for _, result := range r.commands {
		if identity.IdempotencyKey != "" && result.IdempotencyKey == identity.IdempotencyKey {
			return result, nil
		}
	}
	return entity.CommandResult{}, errs.ErrNotFound
}

func (r *memoryRepository) CreateOrganization(_ context.Context, organization entity.Organization, event entity.OutboxEvent, result entity.CommandResult) error {
	r.organizations[organization.ID] = organization
	r.commands[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetOrganization(_ context.Context, id uuid.UUID) (entity.Organization, error) {
	organization, ok := r.organizations[id]
	if !ok {
		return entity.Organization{}, errs.ErrNotFound
	}
	return organization, nil
}

func (r *memoryRepository) CountActiveOwnerOrganizations(_ context.Context) (int, error) {
	var count int
	for _, organization := range r.organizations {
		if organization.Kind == enum.OrganizationKindOwner && organization.Status == enum.OrganizationStatusActive {
			count++
		}
	}
	return count, nil
}

func (r *memoryRepository) CreateUser(_ context.Context, user entity.User, identity entity.UserIdentity, event entity.OutboxEvent) error {
	r.users[user.ID] = user
	r.identities[identityKey(identity.Provider, identity.Subject)] = identity
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetUser(_ context.Context, id uuid.UUID) (entity.User, error) {
	user, ok := r.users[id]
	if !ok {
		return entity.User{}, errs.ErrNotFound
	}
	return user, nil
}

func (r *memoryRepository) GetUserByEmail(_ context.Context, email string) (entity.User, error) {
	for _, user := range r.users {
		if user.PrimaryEmail == email {
			return user, nil
		}
	}
	return entity.User{}, errs.ErrNotFound
}

func (r *memoryRepository) GetUserByIdentity(_ context.Context, provider enum.IdentityProvider, subject string) (entity.User, error) {
	identity, ok := r.identities[identityKey(provider, subject)]
	if !ok {
		return entity.User{}, errs.ErrNotFound
	}
	return r.GetUser(context.Background(), identity.UserID)
}

func (r *memoryRepository) LinkUserIdentity(_ context.Context, identity entity.UserIdentity, event entity.OutboxEvent) error {
	r.identities[identityKey(identity.Provider, identity.Subject)] = identity
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) PutAllowlistEntry(_ context.Context, entry entity.AllowlistEntry, event entity.OutboxEvent) error {
	r.allowlist[allowlistKey(entry.MatchType, entry.Value)] = entry
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) FindAllowlistEntry(_ context.Context, matchType enum.AllowlistMatchType, v string) (entity.AllowlistEntry, error) {
	entry, ok := r.allowlist[allowlistKey(matchType, v)]
	if !ok {
		return entity.AllowlistEntry{}, errs.ErrNotFound
	}
	return entry, nil
}

func (r *memoryRepository) CreateGroup(_ context.Context, group entity.Group, event entity.OutboxEvent, result entity.CommandResult) error {
	r.groups[group.ID] = group
	r.commands[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetGroup(_ context.Context, id uuid.UUID) (entity.Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return entity.Group{}, errs.ErrNotFound
	}
	return group, nil
}

func (r *memoryRepository) SetMembership(_ context.Context, membership entity.Membership, event entity.OutboxEvent) error {
	for id, existing := range r.memberships {
		if sameMembershipIdentity(existing, membership) && id != membership.ID {
			delete(r.memberships, id)
			break
		}
	}
	r.memberships[membership.ID] = membership
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) FindMembership(_ context.Context, identity query.MembershipIdentity) (entity.Membership, error) {
	for _, membership := range r.memberships {
		if membership.SubjectType == identity.SubjectType && membership.SubjectID == identity.SubjectID &&
			membership.TargetType == identity.TargetType && membership.TargetID == identity.TargetID {
			return membership, nil
		}
	}
	return entity.Membership{}, errs.ErrNotFound
}

func (r *memoryRepository) ListMemberships(_ context.Context, filter query.MembershipGraphFilter) ([]entity.Membership, error) {
	var result []entity.Membership
	for _, membership := range r.memberships {
		if string(membership.SubjectType) == filter.Subject.Type && membership.SubjectID.String() == filter.Subject.ID && membership.Status == filter.Status {
			result = append(result, membership)
		}
	}
	return result, nil
}

func (r *memoryRepository) PutExternalProvider(_ context.Context, provider entity.ExternalProvider, event entity.OutboxEvent) error {
	r.providers[provider.ID] = provider
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetExternalProvider(_ context.Context, id uuid.UUID) (entity.ExternalProvider, error) {
	provider, ok := r.providers[id]
	if !ok {
		return entity.ExternalProvider{}, errs.ErrNotFound
	}
	return provider, nil
}

func (r *memoryRepository) GetExternalProviderBySlug(_ context.Context, slug string) (entity.ExternalProvider, error) {
	for _, provider := range r.providers {
		if provider.Slug == slug {
			return provider, nil
		}
	}
	return entity.ExternalProvider{}, errs.ErrNotFound
}

func (r *memoryRepository) RegisterExternalAccount(_ context.Context, account entity.ExternalAccount, event entity.OutboxEvent, result entity.CommandResult) error {
	r.accounts[account.ID] = account
	r.commands[result.Key] = result
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetExternalAccount(_ context.Context, id uuid.UUID) (entity.ExternalAccount, error) {
	account, ok := r.accounts[id]
	if !ok {
		return entity.ExternalAccount{}, errs.ErrNotFound
	}
	return account, nil
}

func (r *memoryRepository) BindExternalAccount(_ context.Context, binding entity.ExternalAccountBinding, event entity.OutboxEvent) error {
	for id, existing := range r.bindings {
		if sameExternalAccountBindingIdentity(existing, binding) && id != binding.ID {
			delete(r.bindings, id)
			break
		}
	}
	r.bindings[binding.ID] = binding
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) FindExternalAccountBinding(_ context.Context, filter query.ExternalAccountUsageFilter) (entity.ExternalAccountBinding, error) {
	for _, binding := range r.bindings {
		if binding.ExternalAccountID == filter.ExternalAccountID && string(binding.UsageScopeType) == filter.UsageScope.Type && binding.UsageScopeID == filter.UsageScope.ID {
			return binding, nil
		}
	}
	return entity.ExternalAccountBinding{}, errs.ErrNotFound
}

func (r *memoryRepository) FindExternalAccountBindingByIdentity(_ context.Context, identity query.ExternalAccountBindingIdentity) (entity.ExternalAccountBinding, error) {
	for _, binding := range r.bindings {
		if binding.ExternalAccountID == identity.ExternalAccountID &&
			string(binding.UsageScopeType) == identity.UsageScope.Type &&
			binding.UsageScopeID == identity.UsageScope.ID {
			return binding, nil
		}
	}
	return entity.ExternalAccountBinding{}, errs.ErrNotFound
}

func (r *memoryRepository) PutSecretBindingRef(_ context.Context, secret entity.SecretBindingRef, event entity.OutboxEvent) error {
	r.secrets[secret.ID] = secret
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetSecretBindingRef(_ context.Context, id uuid.UUID) (entity.SecretBindingRef, error) {
	secret, ok := r.secrets[id]
	if !ok {
		return entity.SecretBindingRef{}, errs.ErrNotFound
	}
	return secret, nil
}

func (r *memoryRepository) PutAccessAction(_ context.Context, action entity.AccessAction, event entity.OutboxEvent) error {
	r.actions[action.Key] = action
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) GetAccessActionByKey(_ context.Context, key string) (entity.AccessAction, error) {
	action, ok := r.actions[key]
	if !ok {
		return entity.AccessAction{}, errs.ErrNotFound
	}
	return action, nil
}

func (r *memoryRepository) PutAccessRule(_ context.Context, rule entity.AccessRule, event entity.OutboxEvent) error {
	for id, existing := range r.rules {
		if sameAccessRuleIdentity(existing, rule) && id != rule.ID {
			delete(r.rules, id)
			break
		}
	}
	r.rules[rule.ID] = rule
	r.events = append(r.events, event)
	return nil
}

func (r *memoryRepository) FindAccessRule(_ context.Context, identity query.AccessRuleIdentity) (entity.AccessRule, error) {
	for _, rule := range r.rules {
		if rule.Effect == identity.Effect &&
			rule.SubjectType == identity.SubjectType &&
			rule.SubjectID == identity.SubjectID &&
			rule.ActionKey == identity.ActionKey &&
			rule.ResourceType == identity.ResourceType &&
			rule.ResourceID == identity.ResourceID &&
			rule.ScopeType == identity.ScopeType &&
			rule.ScopeID == identity.ScopeID {
			return rule, nil
		}
	}
	return entity.AccessRule{}, errs.ErrNotFound
}

func (r *memoryRepository) ListAccessRules(_ context.Context, filter query.AccessRuleFilter) ([]entity.AccessRule, error) {
	var result []entity.AccessRule
	for _, rule := range r.rules {
		if rule.ActionKey != filter.ActionKey || rule.ResourceType != filter.ResourceType {
			continue
		}
		if rule.ResourceID != "" && rule.ResourceID != filter.ResourceID {
			continue
		}
		if rule.ScopeType == "global" && rule.ScopeID == "" {
			// Global policy applies to all scopes.
		} else if rule.ScopeType != filter.Scope.Type || rule.ScopeID != filter.Scope.ID {
			continue
		}
		for _, subject := range filter.Subjects {
			if string(rule.SubjectType) == subject.Type && rule.SubjectID == subject.ID {
				result = append(result, rule)
				break
			}
		}
	}
	return result, nil
}

func (r *memoryRepository) RecordAccessDecision(_ context.Context, audit entity.AccessDecisionAudit, event *entity.OutboxEvent) error {
	r.audits = append(r.audits, audit)
	if event != nil {
		r.events = append(r.events, *event)
	}
	return nil
}

func (r *memoryRepository) seedUser(status enum.UserStatus) entity.User {
	now := fixedClock{}.Now()
	user := entity.User{
		Base:         entity.Base{ID: r.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		PrimaryEmail: "user@example.com", Status: status,
	}
	r.users[user.ID] = user
	return user
}

func (r *memoryRepository) seedSecret(storeType enum.SecretStoreType, storeRef string) entity.SecretBindingRef {
	now := fixedClock{}.Now()
	secret := entity.SecretBindingRef{
		Base:      entity.Base{ID: r.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		StoreType: storeType,
		StoreRef:  storeRef,
	}
	r.secrets[secret.ID] = secret
	return secret
}

func identityKey(provider enum.IdentityProvider, subject string) string {
	return string(provider) + ":" + subject
}

func allowlistKey(matchType enum.AllowlistMatchType, value string) string {
	return string(matchType) + ":" + value
}

func sameMembershipIdentity(a entity.Membership, b entity.Membership) bool {
	return a.SubjectType == b.SubjectType && a.SubjectID == b.SubjectID && a.TargetType == b.TargetType && a.TargetID == b.TargetID
}

func sameExternalAccountBindingIdentity(a entity.ExternalAccountBinding, b entity.ExternalAccountBinding) bool {
	return a.ExternalAccountID == b.ExternalAccountID && a.UsageScopeType == b.UsageScopeType && a.UsageScopeID == b.UsageScopeID
}

func sameAccessRuleIdentity(a entity.AccessRule, b entity.AccessRule) bool {
	return a.Effect == b.Effect &&
		a.SubjectType == b.SubjectType &&
		a.SubjectID == b.SubjectID &&
		a.ActionKey == b.ActionKey &&
		a.ResourceType == b.ResourceType &&
		a.ResourceID == b.ResourceID &&
		a.ScopeType == b.ScopeType &&
		a.ScopeID == b.ScopeID
}

func ptrInt64(value int64) *int64 {
	return &value
}

func commandMeta(key string) value.CommandMeta {
	return value.CommandMeta{IdempotencyKey: key}
}

func TestBootstrapDeniedWithoutAllowlist(t *testing.T) {
	svc := New(newMemoryRepository(), fixedClock{}, newSequenceIDs())
	_, err := svc.BootstrapUserFromIdentity(context.Background(), BootstrapUserFromIdentityInput{
		Provider: enum.IdentityProviderGitHub, Subject: "42", Email: "owner@example.com",
	})
	if !errors.Is(err, errs.ErrUnauthorizedSubject) {
		t.Fatalf("err = %v, want %v", err, errs.ErrUnauthorizedSubject)
	}
}
