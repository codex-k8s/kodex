package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accessaccountsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/access_accounts/v1"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	accessservice "github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

func TestCreateOrganizationMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	organizationID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	service := &fakeAccessService{
		createOrganization: func(_ context.Context, input accessservice.CreateOrganizationInput) (entity.Organization, error) {
			if input.Kind != enum.OrganizationKindClient {
				t.Fatalf("Kind = %q, want client", input.Kind)
			}
			if input.Slug != "client-team" || input.DisplayName != "Client Team" {
				t.Fatalf("unexpected organization identity: slug=%q display=%q", input.Slug, input.DisplayName)
			}
			if input.Meta.IdempotencyKey != "org-client-team" || input.Meta.Actor.ID != "operator-1" {
				t.Fatalf("unexpected meta: %+v", input.Meta)
			}
			if input.Meta.RequestContext.Source != "staff-gateway" || input.Meta.RequestContext.TraceID != "trace-1" {
				t.Fatalf("unexpected request context: %+v", input.Meta.RequestContext)
			}
			return entity.Organization{
				Base:        entity.Base{ID: organizationID, Version: 3},
				Kind:        input.Kind,
				Slug:        input.Slug,
				DisplayName: input.DisplayName,
				Status:      enum.OrganizationStatusActive,
			}, nil
		},
	}

	response, err := NewServer(service).CreateOrganization(context.Background(), &accessaccountsv1.CreateOrganizationRequest{
		Kind:        accessaccountsv1.OrganizationKind_ORGANIZATION_KIND_CLIENT,
		Slug:        "client-team",
		DisplayName: "Client Team",
		Meta: &accessaccountsv1.CommandMeta{
			IdempotencyKey: "org-client-team",
			Actor:          &accessaccountsv1.Actor{Type: "user", Id: "operator-1"},
			RequestContext: &accessaccountsv1.RequestContext{Source: "staff-gateway", TraceId: "trace-1"},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrganization(): %v", err)
	}
	if response.GetOrganizationId() != organizationID.String() {
		t.Fatalf("OrganizationId = %q, want %q", response.GetOrganizationId(), organizationID)
	}
	if response.GetStatus() != accessaccountsv1.OrganizationStatus_ORGANIZATION_STATUS_ACTIVE {
		t.Fatalf("Status = %s, want active", response.GetStatus())
	}
	if response.GetVersion() != 3 {
		t.Fatalf("Version = %d, want 3", response.GetVersion())
	}
}

func TestCreateOrganizationRejectsInvalidTransportEnum(t *testing.T) {
	t.Parallel()

	_, err := NewServer(&fakeAccessService{}).CreateOrganization(context.Background(), &accessaccountsv1.CreateOrganizationRequest{})
	if !errorsIsInvalidArgument(err) {
		t.Fatalf("CreateOrganization() err = %v, want invalid argument", err)
	}
}

func TestUnimplementedBacklogMethodReturnsUnimplemented(t *testing.T) {
	t.Parallel()

	_, err := NewServer(&fakeAccessService{}).UpdateOrganization(context.Background(), &accessaccountsv1.UpdateOrganizationRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("UpdateOrganization() code = %s, want unimplemented", status.Code(err))
	}
}

func TestSetUserStatusMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	expectedVersion := int64(4)
	service := &fakeAccessService{
		setUserStatus: func(_ context.Context, input accessservice.SetUserStatusInput) (entity.User, error) {
			if input.UserID != userID || input.Status != enum.UserStatusBlocked {
				t.Fatalf("input = %+v, want user %s blocked", input, userID)
			}
			if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion || input.Meta.Reason != "risk_block" {
				t.Fatalf("unexpected meta: %+v", input.Meta)
			}
			return entity.User{
				Base:         entity.Base{ID: userID, Version: expectedVersion + 1},
				PrimaryEmail: "owner@example.com",
				DisplayName:  "Owner",
				Status:       input.Status,
				Locale:       "ru",
			}, nil
		},
	}

	response, err := NewServer(service).SetUserStatus(context.Background(), &accessaccountsv1.SetUserStatusRequest{
		UserId: userID.String(),
		Status: accessaccountsv1.UserStatus_USER_STATUS_BLOCKED,
		Meta: &accessaccountsv1.CommandMeta{
			ExpectedVersion: &expectedVersion,
			Reason:          "risk_block",
		},
	})
	if err != nil {
		t.Fatalf("SetUserStatus(): %v", err)
	}
	if response.GetUserId() != userID.String() || response.GetStatus() != accessaccountsv1.UserStatus_USER_STATUS_BLOCKED ||
		response.GetVersion() != expectedVersion+1 || response.GetLocale() != "ru" {
		t.Fatalf("response = %+v, want blocked user", response)
	}
}

func TestListPendingAccessMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	service := &fakeAccessService{
		listPendingAccess: func(_ context.Context, input accessservice.ListPendingAccessInput) (accessservice.ListPendingAccessResult, error) {
			if input.Scope.Type != "organization" || input.Scope.ID != "org-1" || input.Limit != 25 || input.Cursor != "50" {
				t.Fatalf("unexpected input: %+v", input)
			}
			if input.Meta.Actor.ID != "operator-1" {
				t.Fatalf("unexpected actor: %+v", input.Meta.Actor)
			}
			return accessservice.ListPendingAccessResult{
				Items: []entity.PendingAccessItem{{
					ItemID:     userID.String(),
					ItemType:   "user",
					Subject:    value.SubjectRef{Type: "user", ID: userID.String()},
					Status:     "pending",
					ReasonCode: "user_pending",
					CreatedAt:  time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
				}},
				NextCursor: "75",
			}, nil
		},
	}

	response, err := NewServer(service).ListPendingAccess(context.Background(), &accessaccountsv1.ListPendingAccessRequest{
		Scope:  &accessaccountsv1.ScopeRef{Type: "organization", Id: "org-1"},
		Limit:  25,
		Cursor: "50",
		Meta:   &accessaccountsv1.CommandMeta{Actor: &accessaccountsv1.Actor{Type: "user", Id: "operator-1"}},
	})
	if err != nil {
		t.Fatalf("ListPendingAccess(): %v", err)
	}
	if response.GetNextCursor() != "75" || len(response.GetItems()) != 1 ||
		response.GetItems()[0].GetSubject().GetId() != userID.String() ||
		response.GetItems()[0].GetCreatedAt() == "" {
		t.Fatalf("response = %+v, want one pending item with next cursor", response)
	}
}

func TestUpdateExternalProviderMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	providerID := uuid.MustParse("66666666-6666-4666-8666-666666666666")
	expectedVersion := int64(3)
	service := &fakeAccessService{
		updateExternalProvider: func(_ context.Context, input accessservice.UpdateExternalProviderInput) (entity.ExternalProvider, error) {
			if input.ExternalProviderID != providerID ||
				input.ProviderKind != enum.ExternalProviderRepository ||
				input.DisplayName == nil ||
				*input.DisplayName != "GitHub Enterprise" ||
				input.Status != enum.ExternalProviderStatusDisabled {
				t.Fatalf("input = %+v, want disabled GitHub Enterprise provider", input)
			}
			if input.Slug == nil || *input.Slug != "github-enterprise" || input.IconAssetRef == nil || *input.IconAssetRef != "" {
				t.Fatalf("unexpected optional fields: %+v", input)
			}
			if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
				t.Fatalf("unexpected meta: %+v", input.Meta)
			}
			return entity.ExternalProvider{
				Base:         entity.Base{ID: providerID, Version: expectedVersion + 1},
				Slug:         "github-enterprise",
				ProviderKind: input.ProviderKind,
				DisplayName:  *input.DisplayName,
				Status:       input.Status,
			}, nil
		},
	}

	response, err := NewServer(service).UpdateExternalProvider(context.Background(), &accessaccountsv1.UpdateExternalProviderRequest{
		ExternalProviderId: providerID.String(),
		Slug:               ptrString("github-enterprise"),
		ProviderKind:       accessaccountsv1.ExternalProviderKind_EXTERNAL_PROVIDER_KIND_REPOSITORY,
		DisplayName:        ptrString("GitHub Enterprise"),
		IconAssetRef:       ptrString(""),
		Status:             accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_DISABLED,
		Meta:               &accessaccountsv1.CommandMeta{ExpectedVersion: &expectedVersion},
	})
	if err != nil {
		t.Fatalf("UpdateExternalProvider(): %v", err)
	}
	if response.GetExternalProviderId() != providerID.String() ||
		response.GetDisplayName() != "GitHub Enterprise" ||
		response.GetStatus() != accessaccountsv1.ExternalProviderStatus_EXTERNAL_PROVIDER_STATUS_DISABLED ||
		response.GetVersion() != expectedVersion+1 {
		t.Fatalf("response = %+v, want disabled provider", response)
	}
}

func TestUpdateExternalAccountStatusMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	accountID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	providerID := uuid.MustParse("88888888-8888-4888-8888-888888888888")
	expectedVersion := int64(2)
	service := &fakeAccessService{
		updateExternalAccountStatus: func(_ context.Context, input accessservice.UpdateExternalAccountStatusInput) (entity.ExternalAccount, error) {
			if input.ExternalAccountID != accountID || input.Status != enum.ExternalAccountStatusNeedsReauth {
				t.Fatalf("input = %+v, want account %s needs_reauth", input, accountID)
			}
			if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion || input.Meta.Actor.ID != "operator-1" {
				t.Fatalf("unexpected meta: %+v", input.Meta)
			}
			return entity.ExternalAccount{
				Base:               entity.Base{ID: accountID, Version: expectedVersion + 1},
				ExternalProviderID: providerID,
				AccountType:        enum.ExternalAccountBot,
				DisplayName:        "kodex-agent",
				OwnerScopeType:     enum.ExternalAccountScopeGlobal,
				Status:             input.Status,
			}, nil
		},
	}

	response, err := NewServer(service).UpdateExternalAccountStatus(context.Background(), &accessaccountsv1.UpdateExternalAccountStatusRequest{
		ExternalAccountId: accountID.String(),
		Status:            accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_NEEDS_REAUTH,
		Meta: &accessaccountsv1.CommandMeta{
			ExpectedVersion: &expectedVersion,
			Actor:           &accessaccountsv1.Actor{Type: "user", Id: "operator-1"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateExternalAccountStatus(): %v", err)
	}
	if response.GetExternalAccountId() != accountID.String() ||
		response.GetStatus() != accessaccountsv1.ExternalAccountStatus_EXTERNAL_ACCOUNT_STATUS_NEEDS_REAUTH ||
		response.GetVersion() != expectedVersion+1 {
		t.Fatalf("response = %+v, want needs_reauth account", response)
	}
}

func TestDisableExternalAccountBindingMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	bindingID := uuid.MustParse("99999999-9999-4999-8999-999999999999")
	accountID := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	expectedVersion := int64(5)
	service := &fakeAccessService{
		disableExternalAccountBinding: func(_ context.Context, input accessservice.DisableExternalAccountBindingInput) (entity.ExternalAccountBinding, error) {
			if input.ExternalAccountBindingID != bindingID {
				t.Fatalf("binding ID = %s, want %s", input.ExternalAccountBindingID, bindingID)
			}
			if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
				t.Fatalf("unexpected meta: %+v", input.Meta)
			}
			return entity.ExternalAccountBinding{
				Base:              entity.Base{ID: bindingID, Version: expectedVersion + 1},
				ExternalAccountID: accountID,
				UsageScopeType:    enum.ExternalAccountScopeProject,
				UsageScopeID:      "project-1",
				AllowedActionKeys: []string{"provider.issue.write"},
				Status:            enum.ExternalAccountBindingStatusDisabled,
			}, nil
		},
	}

	response, err := NewServer(service).DisableExternalAccountBinding(context.Background(), &accessaccountsv1.DisableExternalAccountBindingRequest{
		ExternalAccountBindingId: bindingID.String(),
		Meta:                     &accessaccountsv1.CommandMeta{ExpectedVersion: &expectedVersion},
	})
	if err != nil {
		t.Fatalf("DisableExternalAccountBinding(): %v", err)
	}
	if response.GetExternalAccountBindingId() != bindingID.String() ||
		response.GetStatus() != accessaccountsv1.ExternalAccountBindingStatus_EXTERNAL_ACCOUNT_BINDING_STATUS_DISABLED ||
		response.GetVersion() != expectedVersion+1 {
		t.Fatalf("response = %+v, want disabled binding", response)
	}
}

func TestExplainAccessMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	auditID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	ruleID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	service := &fakeAccessService{
		explainAccess: func(_ context.Context, input accessservice.ExplainAccessInput) (accessservice.ExplainAccessResult, error) {
			if input.AuditID != auditID {
				t.Fatalf("AuditID = %s, want %s", input.AuditID, auditID)
			}
			if input.Scope.Type != "global" || input.Meta.Actor.ID != "operator-1" {
				t.Fatalf("unexpected explain input: %+v", input)
			}
			return accessservice.ExplainAccessResult{
				Audit: entity.AccessDecisionAudit{
					ID:             auditID,
					Subject:        value.SubjectRef{Type: "user", ID: "user-1"},
					ActionKey:      "project.read",
					Resource:       value.ResourceRef{Type: "project", ID: "project-1"},
					Scope:          value.ScopeRef{Type: "project", ID: "project-1"},
					RequestContext: value.RequestContext{Source: "staff-gateway", TraceID: "trace-1"},
					Decision:       enum.AccessDecisionAllow,
					ReasonCode:     "explicit_allow",
					PolicyVersion:  7,
					CreatedAt:      time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
					Explanation: value.DecisionExplanation{
						MatchedRules: []value.RuleExplanation{{
							RuleID:     ruleID,
							Effect:     string(enum.AccessEffectAllow),
							Subject:    value.SubjectRef{Type: "user", ID: "user-1"},
							ActionKey:  "project.read",
							Scope:      value.ScopeRef{Type: "project", ID: "project-1"},
							Priority:   5,
							ReasonCode: "explicit_allow",
						}},
					},
				},
			}, nil
		},
	}

	response, err := NewServer(service).ExplainAccess(context.Background(), &accessaccountsv1.ExplainAccessRequest{
		AuditId: auditID.String(),
		Scope:   &accessaccountsv1.ScopeRef{Type: "global"},
		Meta:    &accessaccountsv1.CommandMeta{Actor: &accessaccountsv1.Actor{Type: "user", Id: "operator-1"}},
	})
	if err != nil {
		t.Fatalf("ExplainAccess(): %v", err)
	}
	if response.GetAuditId() != auditID.String() || response.GetDecision() != accessaccountsv1.AccessDecision_ACCESS_DECISION_ALLOW {
		t.Fatalf("response = %+v, want audit %s allow", response, auditID)
	}
	if response.GetPolicyVersion() != 7 || len(response.GetMatchedRules()) != 1 || response.GetMatchedRules()[0].GetRuleId() != ruleID.String() {
		t.Fatalf("matched rules = %+v, want rule %s", response.GetMatchedRules(), ruleID)
	}
	if response.GetSubject().GetId() != "user-1" || response.GetActionKey() != "project.read" ||
		response.GetResource().GetId() != "project-1" || response.GetScope().GetType() != "project" ||
		response.GetRequestContext().GetTraceId() != "trace-1" || response.GetCreatedAt() == "" {
		t.Fatalf("incomplete audit response: %+v", response)
	}
}

func errorsIsInvalidArgument(err error) bool {
	return err == errs.ErrInvalidArgument
}

type fakeAccessService struct {
	createOrganization            func(context.Context, accessservice.CreateOrganizationInput) (entity.Organization, error)
	disableExternalAccountBinding func(context.Context, accessservice.DisableExternalAccountBindingInput) (entity.ExternalAccountBinding, error)
	explainAccess                 func(context.Context, accessservice.ExplainAccessInput) (accessservice.ExplainAccessResult, error)
	listPendingAccess             func(context.Context, accessservice.ListPendingAccessInput) (accessservice.ListPendingAccessResult, error)
	setUserStatus                 func(context.Context, accessservice.SetUserStatusInput) (entity.User, error)
	updateExternalAccountStatus   func(context.Context, accessservice.UpdateExternalAccountStatusInput) (entity.ExternalAccount, error)
	updateExternalProvider        func(context.Context, accessservice.UpdateExternalProviderInput) (entity.ExternalProvider, error)
}

func (f *fakeAccessService) BootstrapUserFromIdentity(context.Context, accessservice.BootstrapUserFromIdentityInput) (accessservice.BootstrapUserFromIdentityResult, error) {
	return accessservice.BootstrapUserFromIdentityResult{}, errs.ErrNotFound
}

func (f *fakeAccessService) SetUserStatus(ctx context.Context, input accessservice.SetUserStatusInput) (entity.User, error) {
	if f.setUserStatus != nil {
		return f.setUserStatus(ctx, input)
	}
	return entity.User{}, errs.ErrNotFound
}

func (f *fakeAccessService) CreateOrganization(ctx context.Context, input accessservice.CreateOrganizationInput) (entity.Organization, error) {
	if f.createOrganization != nil {
		return f.createOrganization(ctx, input)
	}
	return entity.Organization{}, errs.ErrNotFound
}

func (f *fakeAccessService) CreateGroup(context.Context, accessservice.CreateGroupInput) (entity.Group, error) {
	return entity.Group{}, errs.ErrNotFound
}

func (f *fakeAccessService) SetMembership(context.Context, accessservice.SetMembershipInput) (entity.Membership, error) {
	return entity.Membership{}, errs.ErrNotFound
}

func (f *fakeAccessService) PutAllowlistEntry(context.Context, accessservice.PutAllowlistEntryInput) (entity.AllowlistEntry, error) {
	return entity.AllowlistEntry{}, errs.ErrNotFound
}

func (f *fakeAccessService) DisableAllowlistEntry(context.Context, accessservice.DisableAllowlistEntryInput) (entity.AllowlistEntry, error) {
	return entity.AllowlistEntry{}, errs.ErrNotFound
}

func (f *fakeAccessService) PutExternalProvider(context.Context, accessservice.PutExternalProviderInput) (entity.ExternalProvider, error) {
	return entity.ExternalProvider{}, errs.ErrNotFound
}

func (f *fakeAccessService) UpdateExternalProvider(ctx context.Context, input accessservice.UpdateExternalProviderInput) (entity.ExternalProvider, error) {
	if f.updateExternalProvider != nil {
		return f.updateExternalProvider(ctx, input)
	}
	return entity.ExternalProvider{}, errs.ErrNotFound
}

func (f *fakeAccessService) RegisterExternalAccount(context.Context, accessservice.RegisterExternalAccountInput) (entity.ExternalAccount, error) {
	return entity.ExternalAccount{}, errs.ErrNotFound
}

func (f *fakeAccessService) UpdateExternalAccountStatus(ctx context.Context, input accessservice.UpdateExternalAccountStatusInput) (entity.ExternalAccount, error) {
	if f.updateExternalAccountStatus != nil {
		return f.updateExternalAccountStatus(ctx, input)
	}
	return entity.ExternalAccount{}, errs.ErrNotFound
}

func (f *fakeAccessService) BindExternalAccount(context.Context, accessservice.BindExternalAccountInput) (entity.ExternalAccountBinding, error) {
	return entity.ExternalAccountBinding{}, errs.ErrNotFound
}

func (f *fakeAccessService) DisableExternalAccountBinding(ctx context.Context, input accessservice.DisableExternalAccountBindingInput) (entity.ExternalAccountBinding, error) {
	if f.disableExternalAccountBinding != nil {
		return f.disableExternalAccountBinding(ctx, input)
	}
	return entity.ExternalAccountBinding{}, errs.ErrNotFound
}

func (f *fakeAccessService) PutAccessAction(context.Context, accessservice.PutAccessActionInput) (entity.AccessAction, error) {
	return entity.AccessAction{}, errs.ErrNotFound
}

func (f *fakeAccessService) PutAccessRule(context.Context, accessservice.PutAccessRuleInput) (entity.AccessRule, error) {
	return entity.AccessRule{}, errs.ErrNotFound
}

func (f *fakeAccessService) CheckAccess(context.Context, accessservice.CheckAccessInput) (accessservice.CheckAccessResult, error) {
	return accessservice.CheckAccessResult{
		Decision:    enum.AccessDecisionDeny,
		ReasonCode:  "test",
		Explanation: value.DecisionExplanation{Decision: string(enum.AccessDecisionDeny), ReasonCode: "test", PolicyVersion: 1},
	}, nil
}

func (f *fakeAccessService) ExplainAccess(ctx context.Context, input accessservice.ExplainAccessInput) (accessservice.ExplainAccessResult, error) {
	if f.explainAccess != nil {
		return f.explainAccess(ctx, input)
	}
	return accessservice.ExplainAccessResult{}, errs.ErrNotFound
}

func (f *fakeAccessService) ListPendingAccess(ctx context.Context, input accessservice.ListPendingAccessInput) (accessservice.ListPendingAccessResult, error) {
	if f.listPendingAccess != nil {
		return f.listPendingAccess(ctx, input)
	}
	return accessservice.ListPendingAccessResult{}, errs.ErrNotFound
}

func (f *fakeAccessService) ResolveExternalAccountUsage(context.Context, accessservice.ResolveExternalAccountUsageInput) (accessservice.ResolveExternalAccountUsageResult, error) {
	return accessservice.ResolveExternalAccountUsageResult{
		ExternalAccount: entity.ExternalAccount{
			Base: entity.Base{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}, nil
}

func ptrString(value string) *string {
	return &value
}
