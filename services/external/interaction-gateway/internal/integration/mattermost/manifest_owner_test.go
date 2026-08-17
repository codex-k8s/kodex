package mattermost

import (
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

func TestResolveOwnerUsesUniqueOrganizationBindingForDynamicProject(t *testing.T) {
	t.Parallel()

	organizationID := uuid.NewString()
	providerUserID := "mattermost-owner"
	current := &index{actors: map[string]ActorBinding{
		"bootstrap": {
			MattermostUserID: providerUserID,
			ActorID:          uuid.NewString(), OrganizationID: organizationID, ProjectID: uuid.NewString(),
		},
	}}
	principal := entity.TeamPrincipal{
		ActorID: uuid.NewString(), OrganizationID: organizationID, ProjectID: uuid.NewString(),
	}

	resolved, err := current.resolveOwner(principal)
	if err != nil {
		t.Fatalf("resolve dynamic project owner: %v", err)
	}
	if resolved.MattermostUserID != providerUserID || resolved.ActorID != principal.ActorID ||
		resolved.ProjectID != principal.ProjectID || resolved.OrganizationID != principal.OrganizationID {
		t.Fatalf("dynamic owner boundary mismatch: %#v", resolved)
	}
}

func TestResolveOwnerRejectsAmbiguousOrganizationBinding(t *testing.T) {
	t.Parallel()

	organizationID := uuid.NewString()
	current := &index{actors: map[string]ActorBinding{
		"first": {
			MattermostUserID: "first-owner", ActorID: uuid.NewString(),
			OrganizationID: organizationID, ProjectID: uuid.NewString(),
		},
		"second": {
			MattermostUserID: "second-owner", ActorID: uuid.NewString(),
			OrganizationID: organizationID, ProjectID: uuid.NewString(),
		},
	}}
	_, err := current.resolveOwner(entity.TeamPrincipal{
		ActorID: uuid.NewString(), OrganizationID: organizationID, ProjectID: uuid.NewString(),
	})
	if err == nil {
		t.Fatal("ambiguous organization owner mapping was accepted")
	}
}
