package platform

import (
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"strings"
	"testing"
	"time"
)

// configurePersonalRepositoryBundleFixture verifies the root's own synthetic credential through the real probe owner path, without creating shared credential state.
// configurePersonalRepositoryBundleFixture проверяет собственный synthetic credential root через настоящий probe owner path без создания общего credential state.
func configurePersonalRepositoryBundleFixture(t *testing.T, f *repositoryBundleFixture) {
	t.Helper()
	f.owner.Permission = "platform.personal-integrations.create"
	binding := f.execute(command.CreatePersonalIntegrationCredential, "personal-binding", nil, command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref, ExpectedConnectionVersion: f.connection.Version}).PersonalIntegrationCredential
	f.owner.Permission = "platform.personal-integrations.configure"
	configured, err := f.service.ConfigurePersonalIntegrationCredential(f.ctx, f.owner, value.Mutation{IdempotencyKey: "bundle-personal-configure", ExpectedVersion: &binding.Version}, f.connection.Ref, f.connection.Version, []byte("synthetic-personal-bundle"))
	if err != nil {
		t.Fatal(err)
	}
	f.owner.Permission = "platform.personal-integrations.test"
	f.execute(command.TestPersonalIntegrationCredential, "personal-test", &configured.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref, ExpectedConnectionVersion: f.connection.Version})
	verifier := f.principal("integration-gateway", "platform.personal-integration-tests.claim")
	claims, err := f.service.ClaimPersonalIntegrationCredentialTests(f.ctx, verifier, "bundle-personal-verifier", 1)
	if err != nil || len(claims) != 1 {
		t.Fatal("claim personal bundle verification", err)
	}
	completion := personalTestCompletionFromClaim(claims[0])
	verifier.Permission = "platform.personal-integration-tests.begin"
	if err := f.service.BeginPersonalIntegrationCredentialProbe(f.ctx, verifier, completion); err != nil {
		t.Fatal(err)
	}
	completion.Outcome = entity.PersonalIntegrationCredentialAuthenticated
	completion.ConfirmedCapabilityKeys = []string{"github.identity.read"}
	verifier.Permission = "platform.personal-integration-tests.complete"
	if _, err := f.service.Execute(f.ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: verifier, Mutation: value.Mutation{IdempotencyKey: "bundle-personal-verified"}, Payload: completion}); err != nil {
		t.Fatal(err)
	}
}

// TestRepositoryBundlePersonalCredentialComponent exercises exact owner proposal/gate/claim/Begin and revoke/rotation cancellation using only synthetic receipts and native PostgreSQL.
// TestRepositoryBundlePersonalCredentialComponent проверяет exact owner proposal/Gate/claim/Begin и отмену revoke/rotation на synthetic receipts и native PostgreSQL.
func TestRepositoryBundlePersonalCredentialComponent(t *testing.T) {
	for _, change := range []string{"", "REVOKE", "ROTATE", "REVOKE_STARTED", "ROTATE_STARTED"} {
		t.Run(change, func(t *testing.T) {
			f := newRepositoryBundleFixtureProfile(t, false, true)
			f.prepare()
			if f.proposal.RepositoryID != 7 || f.proposal.RepositoryFullName != "fixture/repository" {
				t.Fatal("personal proposal omitted sealed repository identity")
			}
			if change != "" {
				started := strings.HasSuffix(change, "_STARTED")
				var original port.RepositoryBundleEffect
				if started {
					f.decide("APPROVE")
					original = f.claim()
					f.begin(original)
				}
				binding, err := f.service.GetPersonalIntegrationCredential(f.ctx, f.owner, f.connection.Ref)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(change, "REVOKE") {
					f.owner.Permission = "platform.personal-integrations.revoke"
					f.execute(command.RevokePersonalIntegrationCredential, "personal-revoke", &binding.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref})
				} else {
					f.owner.Permission = "platform.personal-integrations.configure"
					if _, err := f.service.ConfigurePersonalIntegrationCredential(f.ctx, f.owner, value.Mutation{IdempotencyKey: "bundle-personal-rotate", ExpectedVersion: &binding.Version}, f.connection.Ref, f.connection.Version, []byte("synthetic-replaced-bundle")); err != nil {
						t.Fatal(err)
					}
				}
				claims, err := f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "bundle-personal-revoked", 1)
				if started {
					if err != nil || len(claims) != 0 {
						t.Fatal("personal recovery ignored its bounded backoff", err)
					}
					timer := time.NewTimer(31 * time.Second)
					select {
					case <-timer.C:
					case <-f.ctx.Done():
						timer.Stop()
						t.Fatal("personal recovery backoff exceeded its fixture deadline")
					}
					timer.Stop()
					claims, err = f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "bundle-personal-recovery", 1)
					if err != nil || len(claims) != 1 || claims[0].Mode != "RECOVER_READ_ONLY" {
						t.Fatal("begun personal bundle lost read-only recovery", err)
					}
					if _, _, err := f.service.BeginRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.begin"), original); err == nil {
						t.Fatal("revoked original personal lease repeated Begin")
					}
				} else if err != nil || len(claims) != 0 {
					t.Fatal("revoked personal bundle reached worker", err)
				}
				owner := f.owner
				owner.Permission = "platform.repository-bundles.get"
				proposal, err := f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{ProposalRef: f.proposal.Ref, Limit: 1})
				expected := "CANCELLED"
				if started {
					expected = "UNKNOWN_OUTCOME"
				}
				if err != nil || proposal.Proposal.State != expected {
					t.Fatal("personal bundle did not cancel in owner transaction", err)
				}
				return
			}
			f.decide("APPROVE")
			effect := f.claim()
			f.begin(effect)
			wrong := f.input
			wrong.RepositoryID = 8
			wrong.IdempotencyKey = "bundle-foreign-repository"
			if _, err := f.service.PrepareRepositoryBundle(f.ctx, f.principal("runtime-controller", "platform.repository-bundles.prepare"), wrong); err == nil {
				t.Fatal("second repository reused exact proposal authority")
			}
		})
	}
}
