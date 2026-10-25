package platform

import (
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// testRepositoryBundleUnknownRecovery uses two separate native graphs and one shared real30-second backoff, proving both terminal-root and still-active-root revocation retain bytes without reissuing a native invocation grant.
// testRepositoryBundleUnknownRecovery использует два native graph и один настоящий30-second backoff, проверяя terminal-root и active-root revoke без перевыдачи native invocation grant и утраты source bytes.
func testRepositoryBundleUnknownRecovery(t *testing.T) {
	type pending struct {
		fixture  *repositoryBundleFixture
		effect   port.RepositoryBundleEffect
		terminal bool
	}
	var items []pending
	for _, terminal := range []bool{true, false} {
		f := newRepositoryBundleFixture(t)
		f.prepare()
		f.decide("APPROVE")
		effect := f.claim()
		f.begin(effect)
		failed, err := f.service.FailRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.fail"), effect.Lease, "OUTCOME_UNCONFIRMED")
		if err != nil || failed.State != "UNKNOWN_OUTCOME" {
			t.Fatal("begun uncertainty lost", err)
		}
		if terminal {
			run, err := f.service.GetRun(f.ctx, f.owner, f.run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			f.execute(command.CancelRun, "terminal-root", &run.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Terminal root cannot reopen"})
			a, err := f.service.GetArtifact(f.ctx, f.owner, f.input.Archive.Ref)
			if err != nil {
				t.Fatal(err)
			}
			impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, a.Ref, "DELETE")
			if err != nil {
				t.Fatal(err)
			}
			f.execute(command.DeleteArtifact, "source-delete", &a.Version, command.ArtifactLifecycleInput{ArtifactRef: a.Ref, ImpactDigest: impact.Digest})
			impact, err = f.service.GetArtifactImpact(f.ctx, f.owner, a.Ref, "PURGE")
			if err != nil || impact.Permitted {
				t.Fatal("UNKNOWN source is purgeable", err)
			}
		} else {
			c, err := f.service.GetIntegrationConnection(f.ctx, f.owner, f.connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			f.execute(command.ChangeIntegrationGrant, "revoke-begun", &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: repositoryBundleOperation, AgentRef: f.agent.Ref, Enabled: false, ApprovalPolicy: "HUMAN_EACH_EFFECT"})
		}
		items = append(items, pending{fixture: f, effect: effect, terminal: terminal})
	}
	timer := time.NewTimer(31 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-items[0].fixture.ctx.Done():
		t.Fatal("bounded recovery backoff elapsed")
	}
	for _, item := range items {
		f, effect := item.fixture, item.effect
		works, err := f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "bundle-recovery", 1)
		if err != nil || len(works) != 1 || works[0].Mode != "RECOVER_READ_ONLY" || works[0].Proposal.Ref != f.proposal.Ref || works[0].Proposal.FailureCode != "AUTHORITY_CHANGED" {
			t.Fatal("UNKNOWN lost durable authority closure", err)
		}
		effect.Lease = works[0].Lease
		var state, invocation string
		var retained bool
		var active int
		if err = f.pool.QueryRow(f.ctx, queryRepositoryBundleFixtureState, f.proposal.Ref).Scan(&state, &invocation, &retained, &active); err != nil || invocation != "UNKNOWN_OUTCOME" || active != 0 || !retained {
			t.Fatal("read-only recovery reissued native invocation authority or lost retention", err)
		}
		if _, err = f.service.ReadRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.content.read"), effect.Lease); err == nil {
			t.Fatal("revoked recovery disclosed source bytes")
		}
		if _, _, err = f.service.BeginRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.begin"), effect); err == nil {
			t.Fatal("recovery acquired another write intent")
		}
		f.complete(effect)
		if !item.terminal {
			f.completeOrigin()
		}
		fresh, err := f.service.GetRun(f.ctx, f.owner, f.run.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if item.terminal && fresh.State != "CANCELLED" {
			t.Fatal("past effect revived terminal root")
		}
		_, graph, err := f.service.GetRunGraph(f.ctx, f.owner, f.run.Ref)
		if err != nil {
			t.Fatal(err)
		}
		for _, edge := range graph.Edges {
			if edge.Type == "CONTINUES" {
				t.Fatal("source closure created another writer turn")
			}
		}
		if err = f.pool.QueryRow(f.ctx, queryRepositoryBundleFixtureState, f.proposal.Ref).Scan(&state, &invocation, &retained, &active); err != nil || retained || active != 0 || invocation != "UNKNOWN_OUTCOME" {
			t.Fatal("settled ledger reopened native invocation or leaked retention", err)
		}
		f.noObjectEffects()
	}
}
