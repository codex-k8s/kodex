package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// standardSessionRepositoryFixture exposes only the narrow identity/session ports while its embedded unrelated repository methods remain unavailable.
// standardSessionRepositoryFixture предоставляет лишь narrow identity/session ports; прочие embedded repository methods недоступны.
type standardSessionRepositoryFixture struct {
	platformrepo.Repository
	binding             platformrepo.StandardOIDCBinding
	reads, writes       int
	organization, actor string
	revision            uint64
	closure             bool
}

// ResolveStandardOIDCIdentity is unavailable in a typed self-session query fixture; transport verification must have resolved the principal beforehand.
// ResolveStandardOIDCIdentity недоступен в typed self-session query fixture; transport verification обязана заранее разрешить principal.
func (*standardSessionRepositoryFixture) ResolveStandardOIDCIdentity(context.Context, platformrepo.StandardOIDCIdentity) (platformrepo.StandardOIDCBinding, error) {
	return platformrepo.StandardOIDCBinding{}, errs.ErrForbidden
}

// ReadStandardOIDCSession captures the exact tuple and closure mode chosen by the domain method, not a request payload.
// ReadStandardOIDCSession сохраняет exact tuple и closure mode, выбранные domain method, а не request payload.
func (fixture *standardSessionRepositoryFixture) ReadStandardOIDCSession(_ context.Context, organization, actor string, revision uint64, closure bool) (platformrepo.StandardOIDCBinding, error) {
	fixture.reads++
	fixture.organization, fixture.actor, fixture.revision, fixture.closure = organization, actor, revision, closure
	return fixture.binding, nil
}

// RevokeStandardOIDCSession models the exact atomic owner transition without accepting alternate target selectors.
// RevokeStandardOIDCSession моделирует exact atomic owner transition без альтернативных target selectors.
func (fixture *standardSessionRepositoryFixture) RevokeStandardOIDCSession(_ context.Context, organization, actor string, revision uint64) error {
	fixture.writes++
	fixture.organization, fixture.actor, fixture.revision = organization, actor, revision
	fixture.binding.State = "REVOKED"
	return nil
}

// standardSessionServiceFixture provides a canonical transport principal whose independent owner binding is explicit and synthetic.
// standardSessionServiceFixture задаёт canonical transport principal с независимым explicit synthetic owner binding.
func standardSessionServiceFixture(t *testing.T) (*Service, *standardSessionRepositoryFixture, value.Principal) {
	t.Helper()
	p := value.Principal{ActorID: "20000000-0000-4000-8000-000000000001", AuthorityTenant: "30000000-0000-4000-8000-000000000001", CallerWorkload: "control-api-gateway", CredentialRevision: 27, CorrelationRef: "synthetic-correlation", Permission: platformrepo.StandardOIDCResolveOperation}
	repository := &standardSessionRepositoryFixture{binding: platformrepo.StandardOIDCBinding{ActorID: p.ActorID, OrganizationID: p.AuthorityTenant, Revision: p.CredentialRevision, State: "ACTIVE"}}
	service, err := New(repository)
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, p
}

// TestStandardOIDCSelfMethodsRequireExactPrincipal denies foreign workloads, permissions, project selectors and malformed identity before any owner read or mutation.
// TestStandardOIDCSelfMethodsRequireExactPrincipal отклоняет чужие workloads, permissions, project selectors и malformed identity до owner read и mutation.
func TestStandardOIDCSelfMethodsRequireExactPrincipal(t *testing.T) {
	for _, change := range []func(*value.Principal){
		func(p *value.Principal) { p.CallerWorkload = "secret-broker" }, func(p *value.Principal) { p.Permission = "organization.manage" },
		func(p *value.Principal) { p.ProjectRef = "prj_synthetic0001" }, func(p *value.Principal) { p.ActorID = "invalid" },
		func(p *value.Principal) { p.AuthorityTenant = "invalid" }, func(p *value.Principal) { p.CredentialRevision = 0 },
	} {
		service, repository, p := standardSessionServiceFixture(t)
		change(&p)
		if _, err := service.StandardOIDCSession(t.Context(), p, false); !errors.Is(err, errs.ErrForbidden) || repository.reads != 0 || repository.writes != 0 {
			t.Fatal("invalid self principal reached owner state")
		}
	}
	service, repository, p := standardSessionServiceFixture(t)
	if _, err := service.RevokeOwnStandardOIDCSession(t.Context(), p); !errors.Is(err, errs.ErrForbidden) || repository.writes != 0 {
		t.Fatal("read permission invoked self-session revoke")
	}
	p.Permission = platformrepo.StandardOIDCRevokeOperation
	if binding, err := service.RevokeOwnStandardOIDCSession(t.Context(), p); err != nil || binding.State != "REVOKED" || repository.writes != 1 || repository.reads != 1 || repository.organization != p.AuthorityTenant || repository.actor != p.ActorID || repository.revision != p.CredentialRevision || !repository.closure {
		t.Fatal("self-session revoke did not use its exact verified tuple and tombstone readback")
	}
}

// TestStandardOIDCSelfReadRejectsForeignOwnerResponse prevents a defective port from exposing another actor/tenant/session through an admitted self query.
// TestStandardOIDCSelfReadRejectsForeignOwnerResponse не допускает выдачи другого actor/tenant/session ошибочным port внутри admitted self query.
func TestStandardOIDCSelfReadRejectsForeignOwnerResponse(t *testing.T) {
	service, repository, p := standardSessionServiceFixture(t)
	repository.binding.OrganizationID = "30000000-0000-4000-8000-000000000099"
	if _, err := service.StandardOIDCSession(t.Context(), p, false); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign owner response escaped the self boundary")
	}
}
