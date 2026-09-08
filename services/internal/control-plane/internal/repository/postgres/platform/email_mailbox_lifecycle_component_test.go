package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testMailboxObservationLifecycle(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, original entity.EmailMailboxConfigurationView) {
	t.Helper()
	connectionRef := original.ConnectionRef
	var err error
	original, err = service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, original.Configuration.Ref, original.BoundRevisionRef)
	if err != nil || original.Revision.State != "PUBLISHED" {
		t.Fatalf("exact bound revision: %v", err)
	}
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-tests.claim"}, "integration-gateway")
	stable := func(t *testing.T, expected entity.EmailMailboxConfigurationView) {
		t.Helper()
		work, found, err := repository.ClaimEmailMailboxPublication(ctx, "observation-check")
		if err != nil || !found {
			t.Fatalf("claim publication: %v", err)
		}
		defer repository.ReleaseEmailMailboxPublication(ctx, work)
		if recovered, err := repository.RecoverEmailMailboxPublication(ctx, work); err != nil || recovered {
			t.Fatalf("observation revoked mailbox: %v", err)
		}
		current, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, expected.Configuration.Ref, expected.Revision.Ref)
		if err != nil || current.Publication == nil || current.Publication.State != "READY" || current.Publication.Ref != expected.Publication.Ref || current.Publication.Digest != expected.Publication.Digest || current.Revision.Digest != expected.Revision.Digest || current.BoundRevisionRef != expected.BoundRevisionRef {
			t.Fatalf("immutable mailbox binding changed: %v", err)
		}
	}
	health := func(t *testing.T, key string, success bool) {
		t.Helper()
		current, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &current.Version}, Payload: command.ConnectionInput{Ref: connectionRef}}); err != nil {
			t.Fatalf("queue health: %v", err)
		}
		stable(t, original)
		claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, key, 32)
		if err != nil {
			t.Fatalf("claim health: %v", err)
		}
		var claim map[string]any
		for _, candidate := range claims {
			if stringMap(candidate, "connectionRef") == connectionRef {
				claim = candidate
			}
		}
		if claim == nil {
			t.Fatal("health claim missing")
		}
		payload := command.IntegrationConnectionTestInput{TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: success}
		if !success {
			payload.SafeErrorCode = "INTEGRATION_CREDENTIAL_UNAVAILABLE"
		}
		complete := command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: payload}
		done, err := service.Execute(ctx, complete)
		if err != nil {
			t.Fatalf("complete health: %v", err)
		}
		replay, err := service.Execute(ctx, complete)
		if err != nil || replay.Connection.Version != done.Connection.Version {
			t.Fatalf("health terminal replay: %v", err)
		}
		stable(t, original)
	}
	for index, success := range []bool{false, true} {
		t.Run(fmt.Sprintf("health_%t_preserves_publication", success), func(t *testing.T) { health(t, fmt.Sprintf("mailbox-observation-%d", index), success) })
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-observation-project"}, Payload: command.ProjectInput{Name: "Mailbox observation", Purpose: "Publication lifecycle", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatal(err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "mailbox-observation-agent", "Mailbox observer")
	for index, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("grant_%t_preserves_publication", enabled), func(t *testing.T) {
			current, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("mailbox-observation-grant-%d", index), ExpectedVersion: &current.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: connectionRef, CapabilityKey: "email.message.send", AgentRef: agent.Ref, Enabled: enabled}})
			if err != nil || result.Connection == nil {
				t.Fatalf("grant lifecycle: %v", err)
			}
			stable(t, original)
		})
	}
	t.Run("unexplained_version_drift_is_not_adopted_and_forward_bind_preserves_revision", func(t *testing.T) {
		// Моделируем старый writer: observation изменена без нового CAS acknowledgement.
		if _, err := repository.pool.Exec(ctx, `UPDATE control_plane.integration_connections SET version=version+1 WHERE ref=$1`, connectionRef); err != nil {
			t.Fatal(err)
		}
		before, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
		if err != nil {
			t.Fatal(err)
		}
		// Даже следующий безопасный TEST не должен усыновить уже существующий drift.
		if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-preexisting-drift", ExpectedVersion: &before.Version}, Payload: command.ConnectionInput{Ref: connectionRef}}); err != nil {
			t.Fatal(err)
		}
		// Закрываем ранее созданный TEST через его настоящий owner lease, без provider.
		claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "drift-test-complete", 32)
		if err != nil {
			t.Fatal(err)
		}

		work, found, err := repository.ClaimEmailMailboxPublication(ctx, "legacy-recovery")
		if err != nil || !found {
			t.Fatal(err)
		}
		if recovered, err := repository.RecoverEmailMailboxPublication(ctx, work); err != nil || !recovered {
			t.Fatalf("preexisting drift accepted: %v", err)
		}
		next, found, err := repository.ClaimEmailMailboxPublication(ctx, "legacy-recovery")
		if err != nil || !found || next.Configuration.Revision <= work.Configuration.Revision {
			t.Fatal("forward recovery missing")
		}
		for _, m := range next.Configuration.Mailboxes {
			if m.ConnectionId == connectionRef {
				t.Fatal("unknown drift retained mailbox")
			}
		}
		// Terminal наступает после создания RECOVERY: его exact receipt не даёт
		// служебному version bump сорвать уже назначенную доставку удаления.
		for _, c := range claims {
			if stringMap(c, "connectionRef") == connectionRef {
				_, err = service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "drift-test-complete"}, Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(c, "testRef"), LeaseRef: stringMap(c, "leaseRef"), Fence: stringMap(c, "fence"), Generation: c["generation"].(int64), Success: false, SafeErrorCode: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		completeMailboxLifecycleFixture(t, ctx, repository, service, next)
		if _, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, "", ""); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("removed binding still visible: %v", err)
		}
		historical, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, original.Configuration.Ref, original.Revision.Ref)
		if err != nil || historical.Publication.State != "SUPERSEDED" || historical.BoundRevisionRef != "" || historical.Revision.Digest != original.Revision.Digest {
			t.Fatalf("historical snapshot lost: %v", err)
		}
		historical, err = service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, original.Configuration.Ref, original.Revision.Ref)
		if err != nil {
			t.Fatal(err)
		}
		bind := command.Command{Kind: command.BindEmailMailboxConfiguration, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-forward-rebind", ExpectedVersion: &historical.Configuration.Version}, Payload: command.EmailMailboxInput{ConnectionRef: connectionRef, ExpectedConnectionVersion: historical.ConnectionVersion, Managed: command.ManagedConfigurationInput{ConfigurationRef: historical.Configuration.Ref, RevisionRef: historical.Revision.Ref}}}
		result, err := service.Execute(ctx, bind)
		if err != nil || result.EmailPublication == nil || result.EmailPublication.Ref == original.Publication.Ref || result.EmailPublication.Revision <= next.Configuration.Revision {
			t.Fatalf("forward published revision rebind: %v", err)
		}
		replay, err := service.Execute(ctx, bind)
		if err != nil || replay.EmailPublication.Ref != result.EmailPublication.Ref {
			t.Fatalf("binding lost ACK replay: %v", err)
		}
		rebound, found, err := repository.ClaimEmailMailboxPublication(ctx, "forward-rebind")
		if err != nil || !found {
			t.Fatal(err)
		}
		completeMailboxLifecycleFixture(t, ctx, repository, service, rebound)
		current, err := service.GetEmailMailboxConfiguration(ctx, owner, connectionRef, original.Configuration.Ref, original.Revision.Ref)
		if err != nil || current.Revision.Ref != original.Revision.Ref || current.Revision.Digest != original.Revision.Digest || current.Specification.SMTP.Secret != original.Specification.SMTP.Secret || current.Specification.IMAP.Secret != original.Specification.IMAP.Secret {
			t.Fatalf("forward binding rewrote credential/revision: %v", err)
		}
		original = current
		health(t, "mailbox-forward-health", true)
	})
	// Снимки и цепочка acknowledgement остаются append-only, включая SQL owner.
	t.Run("publication_and_observation_receipts_are_immutable", func(t *testing.T) {
		var count int
		if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.email_mailbox_observation_receipts WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1)`, connectionRef).Scan(&count); err != nil || count < 8 {
			t.Fatalf("observation receipts missing: %v", err)
		}
		for _, statement := range []string{
			`UPDATE control_plane.email_mailbox_publications SET connection_version=connection_version+1 WHERE ref=$1`,
			`UPDATE control_plane.email_mailbox_observation_receipts SET current_version=current_version+1 WHERE publication_ref=$1`,
			`DELETE FROM control_plane.email_mailbox_observation_receipts WHERE publication_ref=$1`,
		} {
			if _, err := repository.pool.Exec(ctx, statement, original.Publication.Ref); err == nil {
				t.Fatal("immutable publication/receipt changed")
			}
		}
	})
	// Recovery читает тот же advisory fence; TEST не может обогнать revoke.
	t.Run("concurrent_revoke_is_not_hidden_by_observation", func(t *testing.T) {
		current, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := repository.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "mailbox-racing-health", ExpectedVersion: &current.Version}, Payload: command.ConnectionInput{Ref: connectionRef}})
			done <- err
		}()
		deadline := time.Now().Add(3 * time.Second)
		for {
			var waiting bool
			if err := repository.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory')`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("health did not wait for publication lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err = tx.Exec(ctx, `UPDATE control_plane.integration_connections SET enabled=false,version=version+1 WHERE ref=$1`, connectionRef); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err = <-done:
			if !errors.Is(err, errs.ErrVersionMismatch) && !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("racing health bypassed revoke: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("racing health did not finish")
		}
	})
}

func completeMailboxLifecycleFixture(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, work entity.EmailMailboxPublicationWork) {
	t.Helper()
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "email-bridge", Operation: "platform.email.configuration.report"}, "email-bridge")
	document, err := mailpolicy.Produce(ctx, work.Configuration, strings.Repeat("a", 64), mailboxOwnerResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.StageEmailMailboxPolicy(ctx, work, document); err != nil {
		t.Fatal(err)
	}
	if err = repository.MarkEmailMailboxApplied(ctx, work); err != nil {
		t.Fatal(err)
	}
	if err = service.ReportEmailConfigurationReadback(ctx, worker, work.Configuration.Revision, api.Digest(work.Configuration)); err != nil {
		t.Fatal(err)
	}
	if err = repository.CompleteEmailMailboxPublication(ctx, work); err != nil {
		t.Fatal(err)
	}
}
