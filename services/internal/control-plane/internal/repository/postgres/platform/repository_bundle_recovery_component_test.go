package platform

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryRepositoryBundleSourceRetention separates legitimate active knowledge bindings from the publication retention consumer without changing business state.
// queryRepositoryBundleSourceRetention отличает legitimate active knowledge bindings от publication retention consumer, не меняя business state.
//
//go:embed testdata/sql/repository_bundle_source_retention.sql
var queryRepositoryBundleSourceRetention string

// testRepositoryBundleUnknownRecovery uses two separate native graphs and one shared real30-second backoff, proving both terminal-root and still-active-root revocation retain bytes without reissuing a native invocation grant.
// testRepositoryBundleUnknownRecovery использует два native graph и один настоящий30-second backoff, проверяя terminal-root и active-root revoke без перевыдачи native invocation grant и утраты source bytes.
func testRepositoryBundleUnknownRecovery(t *testing.T) {
	type pending struct {
		fixture     *repositoryBundleFixture
		effect      port.RepositoryBundleEffect
		terminal    bool
		purgeDigest string
	}
	var items []pending
	for _, terminal := range []bool{true, false} {
		f := newRepositoryBundleFixtureMode(t, !terminal)
		purgeDigest := ""
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
			if err != nil || impact.Permitted || !contains(impact.Blockers, "ARTIFACT_HAS_RETAINED_REVISIONS") {
				t.Fatal("UNKNOWN source is purgeable", err)
			}
			purgeDigest = impact.Digest
			deleted, err := f.service.GetArtifact(f.ctx, f.owner, a.Ref)
			if err != nil || deleted.LifecycleState != "DELETED" {
				t.Fatal("soft delete did not preserve retained source", err)
			}
			if _, err := f.service.PurgeArtifact(f.ctx, f.owner, value.Mutation{IdempotencyKey: "bundle-retained-purge", ExpectedVersion: &deleted.Version}, deleted.Ref, impact.Digest); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("native purge accepted retained source", err)
			}
			unchanged, err := f.service.GetArtifact(f.ctx, f.owner, deleted.Ref)
			if err != nil || unchanged.Version != deleted.Version || unchanged.LifecycleState != "DELETED" {
				t.Fatal("denied purge advanced artifact lifecycle", err)
			}
			assertRepositoryBundleSourceBytes(t, f)
		} else {
			f.completeOrigin()
			repositoryBundleChildState(t, f, 0, "RUNNING")
			root, err := f.service.GetRun(f.ctx, f.owner, f.parentRun.Ref)
			if err != nil || root.State != "RUNNING" {
				t.Fatal("pending publication failed to retain active root after model completion", err)
			}
			c, err := f.service.GetIntegrationConnection(f.ctx, f.owner, f.connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			f.execute(command.ChangeIntegrationGrant, "revoke-begun", &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: repositoryBundleOperation, AgentRef: f.agent.Ref, Enabled: false, ApprovalPolicy: "HUMAN_EACH_EFFECT"})
		}
		items = append(items, pending{fixture: f, effect: effect, terminal: terminal, purgeDigest: purgeDigest})
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
			repositoryBundleChildState(t, f, 0, "RUNNING")
			owner := resolvedTestPrincipal(t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.repository-bundles.get"}, "control-api-gateway")
			view, err := f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
			if err != nil || view.Proposal.State != "SUCCEEDED" || view.Proposal.CandidateCommitSHA != effect.CandidateCommitSHA || view.Proposal.ContinuationState != "BLOCKED" || view.Proposal.ContinuationCode != "CONTINUATION_AUTHORITY_CHANGED" {
				t.Fatal("confirmed past fact lost explicit closed-authority delivery outcome", err)
			}
			root, err := f.service.GetRun(f.ctx, f.owner, f.parentRun.Ref)
			if err != nil || root.State != "RUNNING" {
				t.Fatal("past fact falsely completed or reopened root", err)
			}
			if err = f.pool.QueryRow(f.ctx, queryRepositoryBundleFixtureState, f.proposal.Ref).Scan(&state, &invocation, &retained, &active); err != nil || invocation != "UNKNOWN_OUTCOME" || active != 0 || !retained {
				t.Fatal("blocked result lost active root custody or reissued native authority", err)
			}
			f.execute(command.CancelRun, "resolve-blocked-pastfact", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Resolve revoked continuation without changing confirmed Git fact"})
			view, err = f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
			if err != nil || view.Proposal.State != "SUCCEEDED" || view.Proposal.CandidateCommitSHA != effect.CandidateCommitSHA || view.Proposal.ContinuationState != "CANCELLED" || view.Proposal.ContinuationCode != "" {
				t.Fatal("owner cancel changed Git fact or left blocked delivery", err)
			}
			// Knowledge bindings independently retain sources; release them through native owner commands before checking the broad retention predicate.
			// Knowledge bindings самостоятельно удерживают sources; перед broad retention check снимаем их через native owner commands.
			for i, ref := range []string{f.input.Archive.Ref, f.input.Manifest.Ref} {
				var bindings int64
				var legitimateRetention bool
				if err := f.pool.QueryRow(f.ctx, queryRepositoryBundleSourceRetention, ref, f.proposal.Ref).Scan(&bindings, &legitimateRetention); err != nil || bindings < 1 || !legitimateRetention {
					t.Fatal("legitimate knowledge custody disappeared before native unbind", err)
				}
				artifact, err := f.service.GetArtifact(f.ctx, f.owner, ref)
				if err != nil {
					t.Fatal("read source before native knowledge release", err)
				}
				f.execute(command.ChangeArtifactBinding, fmt.Sprintf("release-recovery-source-%d", i), &artifact.Version, command.ArtifactBindingInput{ArtifactRef: artifact.Ref, AgentRef: f.agent.Ref, Enabled: false})
			}
		}
		fresh, err := f.service.GetRun(f.ctx, f.owner, f.run.Ref)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.State != "CANCELLED" {
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
		assertRepositoryBundleSourceBytes(t, f)
		if item.terminal {
			impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, f.input.Archive.Ref, "PURGE")
			if err != nil || !impact.Permitted || contains(impact.Blockers, "ARTIFACT_HAS_RETAINED_REVISIONS") || impact.Digest == item.purgeDigest {
				t.Fatal("settled publication retained stale purge impact or digest", err)
			}
		}
	}
}

// assertRepositoryBundleSourceBytes checks the original immutable object receipts and complete source bytes after denied purge or read-only recovery; it never grants model or owner download authority.
// assertRepositoryBundleSourceBytes сверяет исходные immutable object receipts и полные source bytes после запрета purge либо read-only recovery; полномочия model/owner download не выдаются.
func assertRepositoryBundleSourceBytes(t *testing.T, f *repositoryBundleFixture) {
	t.Helper()
	for i, pin := range []entity.RepositoryBundleArtifactPin{f.input.Archive, f.input.Manifest} {
		found := false
		for _, receipt := range f.store.receipts {
			if strings.TrimPrefix(receipt.Digest, "sha256:") != pin.SHA256 || receipt.SizeBytes != pin.Size {
				continue
			}
			found = true
			object, err := f.store.Get(f.ctx, receipt.Key, receipt.VersionID)
			if err != nil {
				t.Fatal("retained exact source object unavailable", err)
			}
			raw, readErr := io.ReadAll(io.LimitReader(object.Body, pin.Size+1))
			closeErr := object.Body.Close()
			expected := [][]byte{f.archive, f.manifest}[i]
			if readErr != nil || closeErr != nil || object.Receipt != receipt || !bytes.Equal(raw, expected) || repositorybundle.Digest(raw) != pin.SHA256 {
				t.Fatal("retained source object receipt or bytes changed")
			}
			break
		}
		if !found || f.store.deletes != 0 {
			t.Fatal("source custody receipt missing or object deletion attempted")
		}
	}
}
