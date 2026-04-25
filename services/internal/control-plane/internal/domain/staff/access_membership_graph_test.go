package staff

import (
	"context"
	"testing"

	accessgraphrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/accessgraph"
	entitytypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	enumtypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	querytypes "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

type stubAccessGraphRepository struct {
	organizations           []accessgraphrepo.Organization
	groups                  []accessgraphrepo.UserGroup
	organizationMemberships []accessgraphrepo.OrganizationMembershipView
	userGroupMemberships    []accessgraphrepo.UserGroupMembershipView
}

func (s *stubAccessGraphRepository) EnsureBootstrapOwnerOrganizationMembership(context.Context, string) error {
	return nil
}

func (s *stubAccessGraphRepository) ListOrganizations(context.Context, int) ([]accessgraphrepo.Organization, error) {
	return append([]accessgraphrepo.Organization(nil), s.organizations...), nil
}

func (s *stubAccessGraphRepository) ListGroups(context.Context, int) ([]accessgraphrepo.UserGroup, error) {
	return append([]accessgraphrepo.UserGroup(nil), s.groups...), nil
}

func (s *stubAccessGraphRepository) ListOrganizationMemberships(context.Context, int) ([]accessgraphrepo.OrganizationMembershipView, error) {
	return append([]accessgraphrepo.OrganizationMembershipView(nil), s.organizationMemberships...), nil
}

func (s *stubAccessGraphRepository) ListUserGroupMemberships(context.Context, int) ([]accessgraphrepo.UserGroupMembershipView, error) {
	return append([]accessgraphrepo.UserGroupMembershipView(nil), s.userGroupMemberships...), nil
}

var _ accessgraphrepo.Repository = (*stubAccessGraphRepository)(nil)

func TestGetAccessMembershipGraphRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	service := &Service{
		accessGraph: &stubAccessGraphRepository{},
	}

	_, err := service.GetAccessMembershipGraph(context.Background(), Principal{UserID: "u-1"}, 50)
	if err == nil {
		t.Fatal("expected forbidden error for non-admin principal")
	}
}

func TestGetAccessMembershipGraphReturnsTypedSnapshot(t *testing.T) {
	t.Parallel()

	service := &Service{
		accessGraph: &stubAccessGraphRepository{
			organizations: []entitytypes.Organization{
				{ID: "org-1", Slug: "platform-owner", Name: "Организация платформы"},
			},
			groups: []entitytypes.UserGroup{
				{ID: "group-1", Scope: enumtypes.UserGroupScopeGlobal, Slug: "operators", Name: "Операторы"},
			},
			organizationMemberships: []querytypes.OrganizationMembershipView{
				{OrganizationID: "org-1", UserID: "user-1", Email: "owner@example.com", Role: "owner"},
			},
			userGroupMemberships: []querytypes.UserGroupMembershipView{
				{GroupID: "group-1", UserID: "user-1", Email: "owner@example.com"},
			},
		},
	}

	snapshot, err := service.GetAccessMembershipGraph(context.Background(), Principal{UserID: "user-1", IsPlatformAdmin: true}, 50)
	if err != nil {
		t.Fatalf("GetAccessMembershipGraph() error = %v", err)
	}
	if len(snapshot.Organizations) != 1 || snapshot.Organizations[0].ID != "org-1" {
		t.Fatalf("unexpected organizations snapshot: %#v", snapshot.Organizations)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].ID != "group-1" {
		t.Fatalf("unexpected groups snapshot: %#v", snapshot.Groups)
	}
	if len(snapshot.OrganizationMemberships) != 1 || snapshot.OrganizationMemberships[0].Email != "owner@example.com" {
		t.Fatalf("unexpected organization memberships snapshot: %#v", snapshot.OrganizationMemberships)
	}
	if len(snapshot.UserGroupMemberships) != 1 || snapshot.UserGroupMemberships[0].GroupID != "group-1" {
		t.Fatalf("unexpected user group memberships snapshot: %#v", snapshot.UserGroupMemberships)
	}
}
