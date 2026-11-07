package platform

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// standardOIDCSessionFixture builds an isolated migrated database with explicit synthetic owner mapping and a bounded verified-identity DTO.
// It supplies no JWT, bypass cookie or real provider credential; signature verification is tested by the native verifier suite.
// standardOIDCSessionFixture создаёт isolated migrated database с явным synthetic owner mapping и ограниченным verified-identity DTO.
// Fixture не содержит JWT, bypass cookie и real provider credential; подпись проверяет native verifier suite.
func standardOIDCSessionFixture(t *testing.T) (*Repository, *pgxpool.Pool, platformrepo.StandardOIDCIdentity) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated standard identity session database")
	}
	t.Cleanup(pool.Close)
	policy := standardOIDCComponentPolicy()
	repository := standardOIDCComponentRepository(t, pool, &policy)
	if err := repository.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap standard identity session fixture: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Second)
	return repository, pool, platformrepo.StandardOIDCIdentity{CallerWorkload: "control-api-gateway", Operation: "platform.query.bootstrap.get",
		Issuer: policy.Issuer, Subject: policy.BootstrapSubjects[0], SessionID: "30000000-0000-4000-8000-000000000001",
		TokenID: "40000000-0000-4000-8000-000000000001", AuthenticatedAt: now.Add(-time.Minute), IssuedAt: now,
		ExpiresAt: now.Add(5 * time.Minute), DisplayName: "Synthetic owner", Groups: []string{"synthetic.readers"}}
}

// TestStandardOIDCSessionComponent proves explicit bootstrap, durable replica rejoin, per-session revisions, identity isolation and irreversible revocation.
// Eligibility is checked again after current application bindings change; unknown users never inherit the bootstrap owner's membership.
// TestStandardOIDCSessionComponent проверяет explicit bootstrap, durable replica rejoin, отдельные session revisions, identity isolation и необратимый revoke.
// Eligibility повторно проверяется после изменения application bindings; unknown users не наследуют membership bootstrap owner.
func TestStandardOIDCSessionComponent(t *testing.T) {
	repository, pool, owner := standardOIDCSessionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	stranger := owner
	stranger.Subject = "20000000-0000-4000-8000-000000000002"
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, stranger); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("arbitrary first identity claimed installation ownership")
	}
	first, err := repository.ResolveStandardOIDCIdentity(ctx, owner)
	if err != nil || first.Revision == 0 || first.ActorID == "" || first.OrganizationID == "" || first.State != "ACTIVE" {
		t.Fatalf("explicit owner session was not resolved: %v", err)
	}
	proofInput := platformrepo.ProofPrincipalInput{CallerWorkload: owner.CallerWorkload, Operation: owner.Operation,
		ExternalActorID: owner.Subject, ExternalTenantID: first.OrganizationID, ExternalIssuer: owner.Issuer,
		ExternalSessionID: owner.SessionID, ExternalSessionRevision: first.Revision, ExternalAuthenticatedAt: owner.AuthenticatedAt,
		ExternalIssuedAt: owner.IssuedAt, ExternalGroups: owner.Groups, PlatformManagedOIDC: true}
	resolved, err := repository.resolveProofIdentity(ctx, proofInput)
	if err != nil || resolved.ActorID != first.ActorID || resolved.OrganizationID != first.OrganizationID {
		t.Fatal("mapped platform session did not resolve current application identity")
	}
	legacy := proofInput
	legacy.PlatformManagedOIDC = false
	if _, err := repository.resolveProofIdentity(ctx, legacy); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("platform policy silently fell back to custom claims")
	}
	foreign := proofInput
	foreign.ExternalTenantID = "20000000-0000-4000-8000-000000000099"
	if _, err := repository.resolveProofIdentity(ctx, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("mapped session selected another organization")
	}
	foreign = proofInput
	foreign.ExternalSessionID = "30000000-0000-4000-8000-000000000099"
	if _, err := repository.resolveProofIdentity(ctx, foreign); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("mapped revision accepted a different provider session")
	}
	policy := standardOIDCComponentPolicy()
	replica := standardOIDCComponentRepository(t, pool, &policy)
	joined, err := replica.ResolveStandardOIDCIdentity(ctx, owner)
	if err != nil || joined.Revision != first.Revision || joined.ActorID != first.ActorID || joined.OrganizationID != first.OrganizationID {
		t.Fatal("replica restart changed the durable identity/session binding")
	}
	secondIdentity := owner
	secondIdentity.SessionID = "30000000-0000-4000-8000-000000000002"
	second, err := replica.ResolveStandardOIDCIdentity(ctx, secondIdentity)
	if err != nil || second.Revision <= first.Revision || second.ActorID != first.ActorID {
		t.Fatal("independent session reused an application revision or actor changed")
	}
	if _, err := replica.ResolveStandardOIDCIdentity(ctx, stranger); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unmapped second user inherited platform access")
	}
	var strangerSessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.oidc_identity_sessions WHERE external_subject=$1::uuid`, stranger.Subject).Scan(&strangerSessions); err != nil || strangerSessions != 1 {
		t.Fatal("denied identity observation was lost or granted duplicate sessions")
	}
	for _, invalid := range []platformrepo.StandardOIDCIdentity{
		{CallerWorkload: "untrusted-workload"},
		func() platformrepo.StandardOIDCIdentity {
			v := owner
			v.Issuer = "https://other.example.invalid"
			return v
		}(),
		func() platformrepo.StandardOIDCIdentity { v := owner; v.AuthenticatedAt = time.Time{}; return v }(),
		func() platformrepo.StandardOIDCIdentity {
			v := owner
			v.ExpiresAt = time.Now().Add(-time.Second)
			return v
		}(),
		func() platformrepo.StandardOIDCIdentity {
			v := owner
			v.AuthenticatedAt = time.Now().Add(-2 * time.Hour)
			return v
		}(),
	} {
		if _, err := repository.ResolveStandardOIDCIdentity(ctx, invalid); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("invalid workload/issuer/auth-time/expiry received a session")
		}
	}
	if err := repository.RevokeStandardOIDCSession(ctx, first.OrganizationID, "20000000-0000-4000-8000-000000000099", first.Revision); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("foreign actor revoked another user's session")
	}
	if repository.RevokeStandardOIDCSession(ctx, first.OrganizationID, first.ActorID, first.Revision) != nil ||
		replica.RevokeStandardOIDCSession(ctx, first.OrganizationID, first.ActorID, first.Revision) != nil {
		t.Fatal("exact revocation or lost-response repeat failed")
	}
	if _, err := replica.ResolveStandardOIDCIdentity(ctx, owner); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("revoked identity session reopened on a new replica")
	}
	if _, err := repository.resolveProofIdentity(ctx, proofInput); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("previously mapped principal bypassed current session revocation")
	}
	for _, mutation := range []string{
		`UPDATE control_plane.oidc_identity_sessions SET state='ACTIVE', revoked_at=NULL WHERE state='REVOKED'`,
		`DELETE FROM control_plane.oidc_identity_sessions`,
		`SELECT setval('control_plane.oidc_identity_session_revisions',1,false)`,
	} {
		if _, err := pool.Exec(ctx, mutation); err == nil {
			t.Fatal("runtime SQL reset a session tombstone or revision sequence")
		}
	}
	if _, err := replica.ResolveStandardOIDCIdentity(ctx, secondIdentity); err != nil {
		t.Fatal("revoking one session revoked an unrelated active session")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.access_bindings SET state='REVOKED' WHERE subject_id=$1::uuid`, first.ActorID); err != nil {
		t.Fatal("revoke isolated fixture application bindings")
	}
	if _, err := replica.ResolveStandardOIDCIdentity(ctx, secondIdentity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("session preflight replaced a current application eligibility check")
	}
}

// TestStandardOIDCGroupsComponent proves that signed issuance, not request order or a fresh session ID, controls group observations across replicas.
// Older tokens cannot restore removed groups, and a conflicting same-time snapshot fails closed without partial membership replacement.
// TestStandardOIDCGroupsComponent проверяет управление observations по signed issuance, а не порядку запросов и новому session ID.
// Старые tokens не возвращают удалённые groups; конфликтующий same-time snapshot закрыто отклоняется без частичной замены memberships.
func TestStandardOIDCGroupsComponent(t *testing.T) {
	repository, pool, owner := standardOIDCSessionFixture(t)
	ctx := t.Context()
	first, err := repository.ResolveStandardOIDCIdentity(ctx, owner)
	if err != nil {
		t.Fatalf("resolve initial owner: %v", err)
	}
	newer := owner
	newer.IssuedAt = newer.IssuedAt.Add(time.Second)
	newer.Groups = []string{}
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, newer); err != nil {
		t.Fatalf("advance signed group removal: %v", err)
	}
	stale := owner
	stale.SessionID = "30000000-0000-4000-8000-000000000003"
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, stale); err != nil {
		t.Fatalf("stale evidence must preserve newer groups while checking current access: %v", err)
	}
	conflict := newer
	conflict.Groups = []string{"synthetic.administrators"}
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, conflict); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("conflicting same-issuance groups replaced the authoritative observation")
	}
	var memberships int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.oidc_group_memberships WHERE subject_id=$1::uuid`, first.ActorID).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatal("stale token restored removed groups")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.oidc_identity_observations SET last_issued_at=last_issued_at-interval '1 second' WHERE subject_id=$1::uuid`, first.ActorID); err == nil {
		t.Fatal("runtime SQL rolled back the identity observation watermark")
	}
}

// TestStandardOIDCConcurrentSessionComponent races two native repository replicas against the same initial session and checks exact committed cardinality.
// Database-only retries must converge on one owner and revision; no token endpoint or external side effect participates.
// TestStandardOIDCConcurrentSessionComponent сталкивает два native repository replicas на одной первой сессии и проверяет committed cardinality.
// DB-only retries обязаны сойтись на одном owner и revision; token endpoint и внешние effects не участвуют.
func TestStandardOIDCConcurrentSessionComponent(t *testing.T) {
	repository, pool, owner := standardOIDCSessionFixture(t)
	policy := standardOIDCComponentPolicy()
	replica := standardOIDCComponentRepository(t, pool, &policy)
	var wait sync.WaitGroup
	bindings := make([]platformrepo.StandardOIDCBinding, 2)
	failures := make([]error, 2)
	start := make(chan struct{})
	for index, current := range []*Repository{repository, replica} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			bindings[index], failures[index] = current.ResolveStandardOIDCIdentity(t.Context(), owner)
		}()
	}
	close(start)
	wait.Wait()
	if failures[0] != nil || failures[1] != nil || bindings[0].Revision == 0 || bindings[0].Revision != bindings[1].Revision || bindings[0].ActorID != bindings[1].ActorID {
		t.Fatalf("replicas did not converge on one durable session: %v / %v", failures[0], failures[1])
	}
	var sessions, owners int
	if pool.QueryRow(t.Context(), `SELECT count(*) FROM control_plane.oidc_identity_sessions`).Scan(&sessions) != nil || sessions != 1 ||
		pool.QueryRow(t.Context(), `SELECT count(*) FROM control_plane.owner_claim_contracts WHERE state='CLAIMED'`).Scan(&owners) != nil || owners != 1 {
		t.Fatal("concurrent bootstrap created duplicate committed ownership/session state")
	}
}
