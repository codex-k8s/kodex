//go:build postgres

package admin_test

import (
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
	if _, err := repository.MarkAgentSessionRuntimeApplied(ctx, adminrepo.MarkAgentSessionRuntimeAppliedInput{
		SessionKey: fixture.session.SessionKey, RuntimeRevisionID: revision.ID,
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
	payload, payloadSHA, payloadSize := confirmedArchiveFixture("confirmed-archive-v1")
	completion, err := repository.CompleteAgentSessionTurnWithArchive(ctx, adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, TurnStatus: "succeeded", SessionStatus: "idle",
		FinalMessage: "готово", Artifacts: "{}", CodexSessionID: "codex-session-safe-id",
		SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA, ArchiveSizeBytes: payloadSize,
		ExtendTTLSeconds: 3600,
	})
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
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: invalidTurn.ID, TurnStatus: "succeeded", SessionStatus: "idle",
		Artifacts: "{}", CodexSessionID: "codex-invalid-safe-id", SessionArchiveGzipBase64: payload,
		ArchiveSHA256: strings.Repeat("0", sha256.Size*2), ArchiveSizeBytes: payloadSize,
	}); err == nil {
		t.Fatal("archive with mismatched checksum was accepted")
	}
	var invalidTurnStatus string
	if err := pool.QueryRow(ctx, `select status from matter_codex_agent_session_turns where id = $1`, invalidTurn.ID).Scan(&invalidTurnStatus); err != nil || invalidTurnStatus != "queued" {
		t.Fatalf("invalid archive changed turn: status=%q error=%v", invalidTurnStatus, err)
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")

	replayPayload, replaySHA, replaySize := confirmedArchiveFixture("must-not-replace-confirmed-archive")
	replayed, err := repository.CompleteAgentSessionTurnWithArchive(ctx, adminrepo.CompleteAgentSessionTurnWithArchiveInput{
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, TurnStatus: "failed", SessionStatus: "blocked",
		ErrorMessage: "must not replace terminal state", Artifacts: "{}", CodexSessionID: "different-safe-id",
		SessionArchiveGzipBase64: replayPayload, ArchiveSHA256: replaySHA, ArchiveSizeBytes: replaySize,
	})
	if err != nil || !replayed.AlreadyCompleted || replayed.Archive.SHA256 != payloadSHA || replayed.Turn.Status != "succeeded" {
		t.Fatalf("idempotent completion = %#v, error = %v", replayed, err)
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
		SessionKey: fixture.session.SessionKey, TurnID: turn.ID, TurnStatus: "succeeded", SessionStatus: "idle",
		FinalMessage: "готово", Artifacts: "{}", CodexSessionID: "codex-rollback-safe-id",
		SessionArchiveGzipBase64: payload, ArchiveSHA256: payloadSHA, ArchiveSizeBytes: payloadSize,
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, input); err == nil {
		t.Fatal("synthetic fault did not abort atomic completion")
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 0, "", "queued")
	if _, err := pool.Exec(ctx, `drop trigger matter_codex_test_reject_archive_publish on matter_codex_agent_sessions; drop function matter_codex_test_reject_archive_publish()`); err != nil {
		t.Fatalf("remove atomicity fault: %v", err)
	}
	if _, err := repository.CompleteAgentSessionTurnWithArchive(ctx, input); err != nil {
		t.Fatalf("retry after rolled back failure: %v", err)
	}
	assertConfirmedArchiveState(t, ctx, pool, fixture.session.ID, turn.ID, 1, payloadSHA, "succeeded")
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
	raw := []byte(value)
	digest := sha256.Sum256(raw)
	return base64.StdEncoding.EncodeToString(raw), hex.EncodeToString(digest[:]), int64(len(raw))
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
