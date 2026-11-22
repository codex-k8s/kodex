package emailpolicy

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestMailboxCredentialOwnerRejectsCredentialSubstitution exercises root-user,
// organization, service, revocation and generation boundaries independently of
// Human Gate decisions. It does not claim a database or gateway acceptance test.
// TestMailboxCredentialOwnerRejectsCredentialSubstitution проверяет пользователя
// корня, организацию, служебный субъект, отзыв и поколение независимо от решений
// Human Gate. Это не приёмка пути через БД либо gateway.
func TestMailboxCredentialOwnerRejectsCredentialSubstitution(t *testing.T) {
	personal := entity.EmailMailboxCredentialOwner{
		OrganizationID: "synthetic-org", SubjectID: "root-user", SubjectKind: "USER",
		Policy: entity.IntegrationCredentialInitiatorRequired, Generation: 7, SubjectActive: true,
	}
	shared := personal
	shared.Policy, shared.SubjectID, shared.SubjectKind = entity.IntegrationCredentialProjectShared, "mail-service", "SERVICE"
	user := entity.IntegrationCredentialInitiator{OrganizationID: "synthetic-org", SubjectID: "root-user", SubjectKind: "USER"}
	revoked := personal
	revoked.SubjectActive = false
	cases := []struct {
		name       string
		owner      entity.EmailMailboxCredentialOwner
		initiator  entity.IntegrationCredentialInitiator
		generation int64
		want       error
	}{
		{name: "personal root", owner: personal, initiator: user, generation: 7},
		{name: "approver is not root owner", owner: personal, initiator: entity.IntegrationCredentialInitiator{OrganizationID: user.OrganizationID, SubjectID: "gate-approver", SubjectKind: "USER"}, generation: 7, want: errs.ErrForbidden},
		{name: "project administrator is not root owner", owner: personal, initiator: entity.IntegrationCredentialInitiator{OrganizationID: user.OrganizationID, SubjectID: "project-admin", SubjectKind: "USER"}, generation: 7, want: errs.ErrForbidden},
		{name: "other organization", owner: personal, initiator: entity.IntegrationCredentialInitiator{OrganizationID: "other-org", SubjectID: user.SubjectID, SubjectKind: "USER"}, generation: 7, want: errs.ErrForbidden},
		{name: "personal schedule cannot borrow user", owner: personal, initiator: entity.IntegrationCredentialInitiator{OrganizationID: user.OrganizationID, SubjectID: user.SubjectID, SubjectKind: "SERVICE"}, generation: 7, want: errs.ErrForbidden},
		{name: "missing root", owner: personal, generation: 7, want: errs.ErrForbidden},
		{name: "old generation", owner: personal, initiator: user, generation: 6, want: errs.ErrForbidden},
		{name: "missing expected generation", owner: personal, initiator: user, want: errs.ErrForbidden},
		{name: "shared service selected for user root", owner: shared, initiator: user, generation: 7},
		{name: "explicit service root", owner: shared, initiator: entity.IntegrationCredentialInitiator{OrganizationID: user.OrganizationID, SubjectID: shared.SubjectID, SubjectKind: "SERVICE"}, generation: 7},
		{name: "different service root", owner: shared, initiator: entity.IntegrationCredentialInitiator{OrganizationID: user.OrganizationID, SubjectID: "other-service", SubjectKind: "SERVICE"}, generation: 7, want: errs.ErrForbidden},
		{name: "revoked root", owner: revoked, initiator: user, generation: 7, want: errs.ErrForbidden},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := AuthorizeCredentialOwner(test.owner, test.initiator, test.generation); !errors.Is(err, test.want) {
				t.Fatalf("ownership result = %v, want %v", err, test.want)
			}
		})
	}
	for _, policy := range []entity.IntegrationCredentialPolicy{"", "TOKEN", entity.IntegrationCredentialProjectShared} {
		invalid := personal
		invalid.Policy = policy
		if err := AuthorizeCredentialOwner(invalid, user, 7); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatalf("invalid stored ownership accepted: %v", err)
		}
	}
	encoded, err := json.Marshal(personal)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("private ownership metadata escaped incidental JSON serialization")
	}
}
