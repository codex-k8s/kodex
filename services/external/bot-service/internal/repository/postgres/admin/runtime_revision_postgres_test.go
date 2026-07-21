//go:build postgres

package admin_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRuntimeRevisionAccountAffinityAndAtomicArchiveInvariants(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "runtime_revision_archive")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate runtime revision schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open runtime revision pool: %v", err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	fixture := seedRuntimeRevisionSession(t, ctx, repository, "runtime-atomic")

	revisionInput := adminrepo.EnsureRuntimeRevisionInput{
		Digest:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Manifest:     `{"schema_version":1,"image":{"repository":"agent-runner","digest":"sha256:synthetic"}}`,
		AccountAlias: "primary", AuthorizationRevision: "credential-17",
	}
	revision, err := repository.EnsureRuntimeRevision(ctx, revisionInput)
	if err != nil {
		t.Fatalf("EnsureRuntimeRevision() error = %v", err)
	}
	reused, err := repository.EnsureRuntimeRevision(ctx, revisionInput)
	if err != nil || reused.ID != revision.ID {
		t.Fatalf("EnsureRuntimeRevision() reuse = %#v, error = %v", reused, err)
	}
	conflict := revisionInput
	conflict.Manifest = `{"schema_version":2}`
	if _, err := repository.EnsureRuntimeRevision(ctx, conflict); !errors.Is(err, adminrepo.ErrRuntimeRevisionConflict) {
		t.Fatalf("digest conflict error = %v", err)
	}
	concurrentInput := revisionInput
	concurrentInput.Digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const concurrentWriters = 8
	start := make(chan struct{})
	results := make(chan entity.RuntimeRevision, concurrentWriters)
	errorsChannel := make(chan error, concurrentWriters)
	for range concurrentWriters {
		go func() {
			<-start
			item, ensureErr := repository.EnsureRuntimeRevision(ctx, concurrentInput)
			results <- item
			errorsChannel <- ensureErr
		}()
	}
	close(start)
	var concurrentID int64
	for range concurrentWriters {
		item := <-results
		if ensureErr := <-errorsChannel; ensureErr != nil {
			t.Fatalf("concurrent EnsureRuntimeRevision() error = %v", ensureErr)
		}
		if concurrentID == 0 {
			concurrentID = item.ID
		} else if item.ID != concurrentID {
			t.Fatalf("concurrent revision IDs differ: %d != %d", item.ID, concurrentID)
		}
	}
	if _, err := pool.Exec(ctx, `update matter_codex_runtime_revisions set account_alias = 'secondary' where id = $1`, revision.ID); err == nil {
		t.Fatal("immutable RuntimeRevision accepted UPDATE")
	}
	if _, err := pool.Exec(ctx, `delete from matter_codex_runtime_revisions where id = $1`, revision.ID); err == nil {
		t.Fatal("immutable RuntimeRevision accepted DELETE")
	}
	bindingInput := adminrepo.ObserveRuntimeSecretBindingInput{
		BindingKey: "github:17:github-token", SecretName: "synthetic-github-binding",
		SecretKey: "github-token", IntegritySHA256: strings.Repeat("1", sha256.Size*2),
	}
	firstBinding, err := repository.ObserveRuntimeSecretBinding(ctx, bindingInput)
	if err != nil {
		t.Fatal(err)
	}
	stableBinding, err := repository.ObserveRuntimeSecretBinding(ctx, bindingInput)
	if err != nil || stableBinding.Revision != firstBinding.Revision || !stableBinding.UpdatedAt.Equal(firstBinding.UpdatedAt) {
		t.Fatalf("unchanged ready-check changed binding revision: first=%#v stable=%#v error=%v", firstBinding, stableBinding, err)
	}
	bindingInput.IntegritySHA256 = strings.Repeat("2", sha256.Size*2)
	rotatedBinding, err := repository.ObserveRuntimeSecretBinding(ctx, bindingInput)
	if err != nil || rotatedBinding.Revision != firstBinding.Revision+1 {
		t.Fatalf("same-ref rotation did not increment binding revision: first=%#v rotated=%#v error=%v", firstBinding, rotatedBinding, err)
	}

	if _, _, err := repository.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey: fixture.session.SessionKey, ProjectID: fixture.session.ProjectID,
		ChatID: fixture.session.ChatID, RoleID: fixture.session.RoleID, SessionScope: fixture.session.SessionScope,
		MattermostChannelID: fixture.session.MattermostChannelID, OpenAIAccountName: "secondary",
		TTLSeconds: 3600, Capabilities: "{}",
	}); err != nil {
		t.Fatalf("idempotent session upsert: %v", err)
	}
	persisted, err := repository.GetAgentSession(ctx, fixture.session.SessionKey)
	if err != nil || persisted.OpenAIAccountName != "primary" {
		t.Fatalf("DB account affinity changed through upsert: account=%q error=%v", persisted.OpenAIAccountName, err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set openai_account_name = 'secondary' where id = $1`, fixture.session.ID); err == nil {
		t.Fatal("DB account affinity accepted direct UPDATE")
	}

	if _, err := repository.SetAgentSessionDesiredRuntimeRevision(ctx, adminrepo.SetAgentSessionDesiredRuntimeRevisionInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID,
	}); err != nil {
		t.Fatalf("SetAgentSessionDesiredRuntimeRevision() error = %v", err)
	}
	const runtimeLeaseToken = "runtime-atomic-lease"
	if _, err := repository.AcquireAgentSessionRuntimeLease(ctx, adminrepo.AcquireAgentSessionRuntimeLeaseInput{
		SessionKey: fixture.session.SessionKey, DesiredRuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: 0, LeaseToken: runtimeLeaseToken, LeaseSeconds: 60,
	}); err != nil {
		t.Fatalf("AcquireAgentSessionRuntimeLease() error = %v", err)
	}
	if _, err := repository.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: 0, AppliedPodUID: "pod-runtime-1", LeaseToken: runtimeLeaseToken,
	}); err != nil {
		t.Fatalf("MarkAgentSessionRuntimeApplied() error = %v", err)
	}
	turn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "runtime-atomic-turn", MattermostChannelID: "channel-runtime",
		MattermostPostID: "post-runtime", UserID: "user-runtime", UserName: "developer",
		Message: "synthetic runtime turn", RuntimeRevisionID: revision.ID,
	})
	if err != nil {
		t.Fatalf("CreateAgentSessionTurn() error = %v", err)
	}
	turn, err = repository.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil {
		t.Fatalf("ClaimNextAgentSessionTurn() error = %v", err)
	}
	payload, payloadSHA, payloadSize := confirmedArchiveFixture("confirmed-archive-v1")
	completionInput := adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, RunID: turn.RunID,
		RuntimeRevisionID: revision.ID, PodUID: "pod-runtime-1", TurnStatus: "succeeded", SessionStatus: "idle",
		FinalMessage: "готово", Artifacts: "{}", CodexSessionID: "codex-session-safe-id",
		SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA, ArchiveSizeBytes: payloadSize,
		ExtendTTLSeconds: 3600,
	}
	completion, err := repository.CompleteAgentSessionTurnWithArchive(ctx, completionInput)
	if err != nil {
		t.Fatalf("CompleteAgentSessionTurnWithArchive() error = %v", err)
	}
	if completion.Archive.Version != 1 || completion.Turn.Status != "succeeded" || completion.AlreadyCompleted {
		t.Fatalf("completion = %#v", completion)
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")
	invalidTurn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "runtime-invalid-archive-turn", MattermostChannelID: "channel-runtime",
		MattermostPostID: "post-runtime-invalid", UserID: "user-runtime", UserName: "developer",
		Message: "synthetic invalid archive turn", RuntimeRevisionID: revision.ID,
	})
	if err != nil {
		t.Fatalf("CreateAgentSessionTurn(invalid archive) error = %v", err)
	}
	invalidTurn, err = repository.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil {
		t.Fatalf("ClaimNextAgentSessionTurn(invalid archive) error = %v", err)
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: invalidTurn.ID, RunID: invalidTurn.RunID,
		RuntimeRevisionID: revision.ID, PodUID: "pod-runtime-1", TurnStatus: "succeeded", SessionStatus: "idle",
		Artifacts: "{}", CodexSessionID: "codex-invalid-safe-id", SessionArchiveGzipBase64: payload,
		ArchiveSHA256: strings.Repeat("0", sha256.Size*2), ArchiveSizeBytes: payloadSize,
	}); err == nil {
		t.Fatal("archive with mismatched checksum was accepted")
	}
	corruptPayload := base64.StdEncoding.EncodeToString([]byte("not-a-gzip-ustar"))
	corruptDigest := sha256.Sum256([]byte("not-a-gzip-ustar"))
	for name, invalid := range map[string]adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		"пустой successful archive": {
			SessionKey: fixture.session.SessionKey, TurnID: invalidTurn.ID, RunID: invalidTurn.RunID,
			RuntimeRevisionID: revision.ID, PodUID: "pod-runtime-1", TurnStatus: "succeeded", SessionStatus: "idle", Artifacts: "{}",
		},
		"повреждённый gzip с верными metadata": {
			SessionKey: fixture.session.SessionKey, TurnID: invalidTurn.ID, RunID: invalidTurn.RunID,
			RuntimeRevisionID: revision.ID, PodUID: "pod-runtime-1", TurnStatus: "succeeded", SessionStatus: "idle", Artifacts: "{}",
			SessionArchiveGzipBase64: corruptPayload, ArchiveSHA256: hex.EncodeToString(corruptDigest[:]), ArchiveSizeBytes: int64(len("not-a-gzip-ustar")),
		},
		"частичная metadata": {
			SessionKey: fixture.session.SessionKey, TurnID: invalidTurn.ID, RunID: invalidTurn.RunID,
			RuntimeRevisionID: revision.ID, PodUID: "pod-runtime-1", TurnStatus: "succeeded", SessionStatus: "idle", Artifacts: "{}",
			SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, invalid); err == nil {
				t.Fatal("неканонический архив принят")
			}
			assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")
		})
	}
	var invalidTurnStatus string
	if err := pool.QueryRow(ctx, `select status from matter_codex_agent_session_turns where id = $1`, invalidTurn.ID).Scan(&invalidTurnStatus); err != nil || invalidTurnStatus != "queued" {
		t.Fatalf("invalid archive changed turn: status=%q error=%v", invalidTurnStatus, err)
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")

	replayed, err := repository.CompleteAgentSessionTurnWithArchive(ctx, completionInput)
	if err != nil || !replayed.AlreadyCompleted || replayed.Archive.SHA256 != payloadSHA || replayed.Turn.Status != "succeeded" {
		t.Fatalf("idempotent completion = %#v, error = %v", replayed, err)
	}
	for name, mutate := range map[string]func(*adminrepo.CompleteAgentSessionTurnWithArchiveInput){
		"run ID":   func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.RunID = "wrong-replay-run" },
		"revision": func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.RuntimeRevisionID++ },
		"Pod UID":  func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.PodUID = "wrong-replay-pod" },
	} {
		t.Run("terminal replay "+name, func(t *testing.T) {
			mismatchReplay := completionInput
			mutate(&mismatchReplay)
			if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, mismatchReplay); err == nil {
				t.Fatal("неточный terminal replay принят")
			}
		})
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")
	if _, err := pool.Exec(ctx, `update matter_codex_agent_session_archives set codex_session_id = 'changed' where session_id = $1`, fixture.session.ID); err == nil {
		t.Fatal("confirmed archive accepted UPDATE")
	}
	if _, err := pool.Exec(ctx, `delete from matter_codex_agent_session_archives where session_id = $1`, fixture.session.ID); err == nil {
		t.Fatal("confirmed archive accepted DELETE")
	}
}

func TestAtomicArchiveCompletionRollsBackEveryPartialWrite(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "runtime_archive_rollback")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate archive rollback schema: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open archive rollback pool: %v", err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	fixture := seedRuntimeRevisionSession(t, ctx, repository, "runtime-rollback")
	turn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "runtime-rollback-turn", MattermostChannelID: "channel-runtime",
		MattermostPostID: "post-runtime", UserID: "user-runtime", UserName: "developer", Message: "synthetic rollback turn",
	})
	if err != nil {
		t.Fatalf("CreateAgentSessionTurn() error = %v", err)
	}
	turn, err = repository.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil {
		t.Fatalf("ClaimNextAgentSessionTurn() error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
create function matter_codex_test_reject_archive_publish()
returns trigger language plpgsql as $$
begin
	raise exception 'synthetic archive publication failure';
end
$$;
create trigger matter_codex_test_reject_archive_publish
before update of archive_version on matter_codex_agent_sessions
for each row execute function matter_codex_test_reject_archive_publish();
`); err != nil {
		t.Fatalf("install atomicity fault: %v", err)
	}
	payload, payloadSHA, payloadSize := confirmedArchiveFixture("rollback-archive")
	input := adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, RunID: turn.RunID, TurnStatus: "succeeded", SessionStatus: "idle",
		FinalMessage: "готово", Artifacts: "{}", CodexSessionID: "codex-rollback-safe-id",
		SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA, ArchiveSizeBytes: payloadSize,
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, input); err == nil {
		t.Fatal("synthetic fault did not abort atomic completion")
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 0, "", "running")
	if _, err := pool.Exec(ctx, `drop trigger matter_codex_test_reject_archive_publish on matter_codex_agent_sessions; drop function matter_codex_test_reject_archive_publish()`); err != nil {
		t.Fatalf("remove atomicity fault: %v", err)
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, input); err != nil {
		t.Fatalf("retry after rolled back failure: %v", err)
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")
}

func TestRuntimeReconcileLeaseFencesConcurrentClaimAndAppliedPublication(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "runtime_reconcile_race")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrate runtime reconcile race schema: %v", err)
	}
	firstPool := newSingleConnectionPool(t, ctx, dsn)
	defer firstPool.Close()
	secondPool := newSingleConnectionPool(t, ctx, dsn)
	defer secondPool.Close()
	first := postgresrepo.NewRepository(firstPool)
	second := postgresrepo.NewRepository(secondPool)
	fixture := seedRuntimeRevisionSession(t, ctx, first, "runtime-reconcile-race")
	revision, err := first.EnsureRuntimeRevision(ctx, adminrepo.EnsureRuntimeRevisionInput{
		Digest: strings.Repeat("c", sha256.Size*2), Manifest: `{"schema_version":1}`,
		AccountAlias: "primary", AuthorizationRevision: "binding:r1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetAgentSessionDesiredRuntimeRevision(ctx, adminrepo.SetAgentSessionDesiredRuntimeRevisionInput{SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AcquireAgentSessionRuntimeLease(ctx, adminrepo.AcquireAgentSessionRuntimeLeaseInput{
		SessionKey: fixture.session.SessionKey, DesiredRuntimeRevisionID: revision.ID, LeaseToken: "initial-lease", LeaseSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID, AppliedPodUID: "pod-race-1", LeaseToken: "initial-lease",
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := first.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "runtime-race-run", MattermostChannelID: "channel-runtime",
		MattermostPostID: "runtime-race-post", UserID: "runtime-user", UserName: "developer",
		Message: "runtime race", RuntimeRevisionID: revision.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.AcquireAgentSessionRuntimeLease(ctx, adminrepo.AcquireAgentSessionRuntimeLeaseInput{
		SessionKey: fixture.session.SessionKey, DesiredRuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: revision.ID, ExpectedPodUID: "pod-race-1",
		LeaseToken: "recreate-lease", LeaseSeconds: 60,
	}); err != nil {
		t.Fatalf("first connection did not acquire reconciliation fence: %v", err)
	}
	if _, err := second.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey); !errors.Is(err, adminrepo.ErrNotFound) {
		t.Fatalf("second connection claimed through reconciliation fence: turn=%d error=%v", turn.ID, err)
	}
	if _, err := second.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: revision.ID, ExpectedPodUID: "pod-race-1",
		AppliedPodUID: "replacement-pod", LeaseToken: "stale-lease",
	}); !errors.Is(err, adminrepo.ErrRuntimeReconciliationConflict) {
		t.Fatalf("stale applied publication crossed CAS fence: %v", err)
	}
	if _, err := first.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: revision.ID, ExpectedPodUID: "pod-race-1",
		AppliedPodUID: "pod-race-1", LeaseToken: "recreate-lease",
	}); err != nil {
		t.Fatalf("exact applied publication failed: %v", err)
	}
	claimed, err := second.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil || claimed.ID != turn.ID {
		t.Fatalf("claim after lease publication = %#v, error=%v", claimed, err)
	}
	if _, err := first.AcquireAgentSessionRuntimeLease(ctx, adminrepo.AcquireAgentSessionRuntimeLeaseInput{
		SessionKey: fixture.session.SessionKey, DesiredRuntimeRevisionID: revision.ID,
		ExpectedAppliedRuntimeRevisionID: revision.ID, ExpectedPodUID: "pod-race-1",
		LeaseToken: "active-turn-lease", LeaseSeconds: 60,
	}); !errors.Is(err, adminrepo.ErrRuntimeReconciliationConflict) {
		t.Fatalf("running turn did not fence reconciliation: %v", err)
	}
}

func TestCompletionRequiresExactRunningTurnAndRuntimeFences(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "runtime_completion_fences")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	fixture := seedRuntimeRevisionSession(t, ctx, repository, "runtime-completion-fences")
	revision, err := repository.EnsureRuntimeRevision(ctx, adminrepo.EnsureRuntimeRevisionInput{
		Digest: strings.Repeat("d", sha256.Size*2), Manifest: `{"schema_version":1}`,
		AccountAlias: "primary", AuthorizationRevision: "binding:r1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.SetAgentSessionDesiredRuntimeRevision(ctx, adminrepo.SetAgentSessionDesiredRuntimeRevisionInput{SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AcquireAgentSessionRuntimeLease(ctx, adminrepo.AcquireAgentSessionRuntimeLeaseInput{
		SessionKey: fixture.session.SessionKey, DesiredRuntimeRevisionID: revision.ID, LeaseToken: "completion-lease", LeaseSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID, AppliedPodUID: "pod-completion-1", LeaseToken: "completion-lease",
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "completion-run", MattermostChannelID: "channel-runtime",
		MattermostPostID: "completion-post", UserID: "runtime-user", UserName: "developer",
		Message: "completion fences", RuntimeRevisionID: revision.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, payloadSHA, payloadSize := confirmedArchiveFixture("completion-fences")
	valid := adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, RunID: turn.RunID,
		RuntimeRevisionID: revision.ID, PodUID: "pod-completion-1", TurnStatus: "succeeded", SessionStatus: "idle",
		FinalMessage: "готово", Artifacts: "{}", CodexSessionID: "codex-completion",
		SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA, ArchiveSizeBytes: payloadSize,
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, valid); err == nil {
		t.Fatal("queued turn completion was accepted")
	}
	turn, err = repository.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	valid.RunID = turn.RunID
	for name, mutate := range map[string]func(*adminrepo.CompleteAgentSessionTurnWithArchiveInput){
		"wrong run ID":        func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.RunID = "wrong-run" },
		"stale revision":      func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.RuntimeRevisionID++ },
		"replacement pod UID": func(input *adminrepo.CompleteAgentSessionTurnWithArchiveInput) { input.PodUID = "replacement-pod" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, invalid); err == nil {
				t.Fatal("completion crossed exact fence")
			}
		})
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = null, active_run_id = '' where id = $1`, fixture.session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, valid); err == nil {
		t.Fatal("non-active running turn completion was accepted")
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = $3 where id = $1`, fixture.session.ID, turn.ID, turn.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, valid); err != nil {
		t.Fatalf("exact completion failed: %v", err)
	}
}

func TestNMinusOneCompletionComputesMissingArchiveMetadata(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "runtime_n_minus_one_completion")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := postgresrepo.NewRepository(pool)
	fixture := seedRuntimeRevisionSession(t, ctx, repository, "runtime-n-minus-one")
	turn, err := repository.CreateAgentSessionTurn(ctx, adminrepo.CreateAgentSessionTurnInput{
		SessionID: fixture.session.ID, RunID: "n-minus-one-run", MattermostChannelID: "channel-runtime",
		MattermostPostID: "n-minus-one-post", UserID: "runtime-user", UserName: "developer", Message: "legacy bounded completion",
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, err = repository.ClaimNextAgentSessionTurn(ctx, fixture.session.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	payload, expectedSHA, expectedSize := confirmedArchiveFixture("n-minus-one-wire")
	completion, err := repository.CompleteAgentSessionTurnWithArchive(ctx, adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, RunID: turn.RunID,
		TurnStatus: "succeeded", SessionStatus: "idle", FinalMessage: "готово", Artifacts: "{}",
		CodexSessionID: "legacy-codex-session", SessionArchiveGzipBase64: payload,
	})
	if err != nil {
		t.Fatalf("N-1 completion without metadata failed: %v", err)
	}
	if completion.Archive.SHA256 != expectedSHA || completion.Archive.SizeBytes != expectedSize {
		t.Fatalf("server-side archive metadata = %#v", completion.Archive)
	}
}

func newSingleConnectionPool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

type runtimeRevisionSessionFixture struct {
	session entity.AgentSession
}

func seedRuntimeRevisionSession(t *testing.T, ctx context.Context, repository *postgresrepo.Repository, key string) runtimeRevisionSessionFixture {
	t.Helper()
	project, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{
		Name: "Runtime revision test", Slug: key, AdvancedSettings: "{}",
	})
	if err != nil {
		t.Fatalf("UpsertProject() error = %v", err)
	}
	role, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: project.ID, Name: "developer", RoleType: "worker", PromptMode: "template",
		OpenAIAccountName: "primary", KubernetesAccess: "read-only", SandboxMode: "danger-full-access",
		AdvancedSettings: "{}", Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertAgentRole() error = %v", err)
	}
	chat, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: project.ID, MattermostChannelID: "channel-runtime", Name: "Runtime chat", Slug: "runtime-chat",
		ChatType: "custom", Settings: "{}", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		t.Fatalf("CreateChat() error = %v", err)
	}
	session, _, err := repository.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey: key, ProjectID: project.ID, ChatID: chat.ID, RoleID: role.ID, SessionScope: "thread",
		MattermostChannelID: "channel-runtime", OpenAIAccountName: "primary", TTLSeconds: 3600, Capabilities: "{}",
	})
	if err != nil {
		t.Fatalf("UpsertAgentSession() error = %v", err)
	}
	return runtimeRevisionSessionFixture{session: session}
}

func confirmedArchiveFixture(value string) (string, string, int64) {
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)
	root := &tar.Header{Name: "sessions", Typeflag: tar.TypeDir, Mode: 0o700, Format: tar.FormatUSTAR}
	if err := tarWriter.WriteHeader(root); err != nil {
		panic(err)
	}
	body := []byte(value)
	file := &tar.Header{Name: "sessions/state.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body)), Format: tar.FormatUSTAR}
	if err := tarWriter.WriteHeader(file); err != nil {
		panic(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		panic(err)
	}
	if err := tarWriter.Close(); err != nil {
		panic(err)
	}
	if err := gzipWriter.Close(); err != nil {
		panic(err)
	}
	compressed := raw.Bytes()
	digest := sha256.Sum256(compressed)
	return base64.StdEncoding.EncodeToString(compressed), hex.EncodeToString(digest[:]), int64(len(compressed))
}

func assertConfirmedArchiveState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID int64, turnID int64, archiveCount int, archiveSHA string, turnStatus string) {
	t.Helper()
	var actualCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_session_archives where session_id = $1`, sessionID).Scan(&actualCount); err != nil {
		t.Fatalf("count confirmed archives: %v", err)
	}
	if actualCount != archiveCount {
		t.Fatalf("confirmed archive count = %d, want %d", actualCount, archiveCount)
	}
	var actualVersion int64
	var actualSHA, actualTurnStatus string
	if err := pool.QueryRow(ctx, `select archive_version, archive_sha256 from matter_codex_agent_sessions where id = $1`, sessionID).Scan(&actualVersion, &actualSHA); err != nil {
		t.Fatalf("read session archive metadata: %v", err)
	}
	if err := pool.QueryRow(ctx, `select status from matter_codex_agent_session_turns where id = $1`, turnID).Scan(&actualTurnStatus); err != nil {
		t.Fatalf("read terminal turn status: %v", err)
	}
	if actualVersion != int64(archiveCount) || actualSHA != archiveSHA || actualTurnStatus != turnStatus {
		t.Fatalf("atomic state: version=%d sha=%q turn=%q", actualVersion, actualSHA, actualTurnStatus)
	}
}
