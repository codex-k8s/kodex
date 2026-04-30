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

	_, err := NewServer(&fakeAccessService{}).SetUserStatus(context.Background(), &accessaccountsv1.SetUserStatusRequest{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("SetUserStatus() code = %s, want unimplemented", status.Code(err))
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
	createOrganization func(context.Context, accessservice.CreateOrganizationInput) (entity.Organization, error)
	explainAccess      func(context.Context, accessservice.ExplainAccessInput) (accessservice.ExplainAccessResult, error)
}

func (f *fakeAccessService) BootstrapUserFromIdentity(context.Context, accessservice.BootstrapUserFromIdentityInput) (accessservice.BootstrapUserFromIdentityResult, error) {
	return accessservice.BootstrapUserFromIdentityResult{}, errs.ErrNotFound
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

func (f *fakeAccessService) PutExternalProvider(context.Context, accessservice.PutExternalProviderInput) (entity.ExternalProvider, error) {
	return entity.ExternalProvider{}, errs.ErrNotFound
}

func (f *fakeAccessService) RegisterExternalAccount(context.Context, accessservice.RegisterExternalAccountInput) (entity.ExternalAccount, error) {
	return entity.ExternalAccount{}, errs.ErrNotFound
}

func (f *fakeAccessService) BindExternalAccount(context.Context, accessservice.BindExternalAccountInput) (entity.ExternalAccountBinding, error) {
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

func (f *fakeAccessService) ResolveExternalAccountUsage(context.Context, accessservice.ResolveExternalAccountUsageInput) (accessservice.ResolveExternalAccountUsageResult, error) {
	return accessservice.ResolveExternalAccountUsageResult{
		ExternalAccount: entity.ExternalAccount{
			Base: entity.Base{ID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}, nil
}
