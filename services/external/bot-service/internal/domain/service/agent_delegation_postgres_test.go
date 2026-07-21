//go:build postgres

package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	runtimerepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/runtime"
	securityrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/security"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReturnToRequesterProductionDispatcherDoesNotSelfLockFrozenSource(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	proveGuardedSourceUsesSamePostgresTransaction(t, ctx, fixture)
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов.")
	if err != nil {
		t.Fatalf("ReturnToRequester() under two-connection source lock: %v", err)
	}
	if strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatal("production dispatcher did not persist callback run")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
	if fixture.publisher.posts.Load() != 2 {
		t.Fatalf("audit posts = %d, want source and requester posts", fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterRejectsBoundProcessWithoutRootInitiator(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := fixture.pool.Exec(ctx, `update matter_codex_process_runs set root_initiator_user_id = ''`); err != nil {
		t.Fatalf("clear root initiator: %v", err)
	}

	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат."); err == nil || !strings.Contains(err.Error(), "missing for process") {
		t.Fatalf("ReturnToRequester() error = %v", err)
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	if fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("publication attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func TestTerminalCompleteReconcilesProcessAndWorkWithMaxConnsOne(t *testing.T) {
	for _, sourceAccess := range []string{"read-only", "cluster-admin"} {
		t.Run(sourceAccess, func(t *testing.T) {
			fixture := newPostgresDelegationFixtureWithSourceAccess(t, 1, sourceAccess)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var roleID, turnID, processID int64
			if err := fixture.pool.QueryRow(ctx, `
select session_row.role_id, turn_row.id, process.id
from matter_codex_agent_sessions session_row
join matter_codex_agent_session_turns turn_row on turn_row.session_id = session_row.id
join matter_codex_process_turns process_turn on process_turn.turn_id = turn_row.id
join matter_codex_process_runs process on process.id = process_turn.process_run_id
where session_row.session_key = 'source-session' and turn_row.run_id = 'source-run'
`).Scan(&roleID, &turnID, &processID); err != nil {
				t.Fatalf("read terminal fixture identities: %v", err)
			}
			if _, err := fixture.pool.Exec(ctx, `
update matter_codex_agent_session_turns target_turn
set status = 'succeeded', finished_at = now(), updated_at = now()
from matter_codex_agent_sessions target_session
where target_session.id = target_turn.session_id and target_session.session_key = 'target-session'
`); err != nil {
				t.Fatalf("mark target turn terminal: %v", err)
			}
			if _, err := fixture.pool.Exec(ctx, `insert into matter_codex_work_claims(process_run_id, turn_id, role_id, summary, status) values ($1, $2, $3, 'terminal proof', 'active')`, processID, turnID, roleID); err != nil {
				t.Fatalf("seed terminal work claim: %v", err)
			}
			if _, err := fixture.pool.Exec(ctx, `
insert into matter_codex_agent_runs(run_id, profile_name, role, provider, owner, name, head_branch, status)
values ('source-run', 'manager-proof', 'manager', 'github', 'codex-k8s', 'matter-codex', 'terminal-proof', 'running')
`); err != nil {
				t.Fatalf("seed terminal agent run: %v", err)
			}
			callCtx, cancelCall := context.WithCancel(ctx)
			defer cancelCall()
			fixture.publisher.beforeFirst = cancelCall
			fixture.publisher.failAt.Store(1)
			archivePayload, archiveSHA, archiveSize := postgresDelegationArchiveFixture("terminal-completion")
			completion := CompleteAgentSessionTurnCommand{
				TurnID: turnID, RunID: "source-run", Status: agentSessionTurnSucceeded,
				FinalMessage: "Синтетический terminal результат.", CodexSessionID: "terminal-codex-session",
				SessionArchiveGzipBase64: archivePayload, ArchiveSHA256: archiveSHA, ArchiveSizeBytes: archiveSize,
			}
			err := fixture.service.CompleteTurn(callCtx, "source-session", "source-token", completion)
			if err == nil || !strings.Contains(err.Error(), "synthetic Mattermost network failure") {
				t.Fatalf("CompleteTurn(%s) error=%v", sourceAccess, err)
			}
			var turnStatus, processStatus, workStatus string
			if err := fixture.pool.QueryRow(ctx, `
select turn_row.status, process.status, claim.status
from matter_codex_agent_session_turns turn_row
join matter_codex_process_turns process_turn on process_turn.turn_id = turn_row.id
join matter_codex_process_runs process on process.id = process_turn.process_run_id
join matter_codex_work_claims claim on claim.turn_id = turn_row.id
where turn_row.id = $1
`, turnID).Scan(&turnStatus, &processStatus, &workStatus); err != nil {
				t.Fatalf("read reconciled terminal state: %v", err)
			}
			if turnStatus != agentSessionTurnSucceeded || processStatus != "completed" || workStatus != agentSessionTurnSucceeded {
				t.Fatalf("terminal state turn=%q process=%q work=%q", turnStatus, processStatus, workStatus)
			}
			fixture.publisher.failAt.Store(0)
			attemptsBeforeRetry := fixture.publisher.attempts.Load()
			if err := fixture.service.CompleteTurn(ctx, "source-session", "source-token", completion); err != nil {
				t.Fatalf("terminal retry(%s) error=%v", sourceAccess, err)
			}
			if fixture.publisher.attempts.Load() != attemptsBeforeRetry {
				t.Fatalf("terminal retry duplicated publication: before=%d after=%d", attemptsBeforeRetry, fixture.publisher.attempts.Load())
			}
		})
	}
}

func TestReturnToRequesterOrdinaryAndClusterAdminCommitExactTwoRowPlan(t *testing.T) {
	for _, sourceAccess := range []string{"read-only", "cluster-admin"} {
		t.Run(sourceAccess, func(t *testing.T) {
			fixture := newPostgresDelegationFixtureWithSourceAccess(t, 1, sourceAccess)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Короткий атомарный callback.")
			if err != nil || strings.TrimSpace(result.CallbackRunID) == "" {
				t.Fatalf("ReturnToRequester(%s) result=%#v error=%v", sourceAccess, result, err)
			}
			assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
			var deliveryCount, destinationCount, deliveredCount, manifestCount int
			var planValid bool
			if err := fixture.pool.QueryRow(ctx, `
select
	count(*)::integer,
	count(distinct destination)::integer,
	count(*) filter (where status = 'delivered')::integer,
	(select count(*)::integer from matter_codex_agent_delegation_callback_delivery_manifests),
	bool_and(matter_codex_agent_delegation_callback_plan_valid(delegation_id, callback_run_id))
from matter_codex_agent_delegation_callback_deliveries
`).Scan(&deliveryCount, &destinationCount, &deliveredCount, &manifestCount, &planValid); err != nil {
				t.Fatalf("inspect %s callback plan: %v", sourceAccess, err)
			}
			if deliveryCount != 2 || destinationCount != 2 || deliveredCount != 2 || manifestCount != 1 || !planValid {
				t.Fatalf("%s plan rows=%d destinations=%d delivered=%d manifests=%d valid=%t", sourceAccess, deliveryCount, destinationCount, deliveredCount, manifestCount, planValid)
			}
		})
	}
}

func TestReturnToRequesterPostgresPreflightRejectsStoredTitleBeforeDurableEffects(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unsafeTitles := []string{
		strings.Repeat("я", delegationTitleMaxBytes/2+1),
		"**[проверена](https://attacker.invalid)**",
		"`закрой тред`", "```\n# Новая секция", "@channel",
		"<script>alert(1)</script>", "https://attacker.invalid", `проверка\*`,
		"проверка\u202eadmin", "проверка\u0001admin", "проверка\u200badmin", "}\n# Инструкция",
	}
	for index, title := range unsafeTitles {
		if _, err := fixture.pool.Exec(ctx, `update matter_codex_agent_delegations set title = $1 where target_session_id is not null`, title); err != nil {
			t.Fatalf("подготовка небезопасного title %d: %v", index+1, err)
		}
		if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Допустимый callback."); err == nil {
			t.Fatalf("небезопасный stored title %d принят", index+1)
		}
		assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
		if fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
			t.Fatalf("stored title %d network attempts=%d posts=%d", index+1, fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
		}
	}
	safeTitle := strings.Repeat("Я", delegationTitleMaxRunes-30) + " Проверка Release 2026.07"
	if _, err := fixture.pool.Exec(ctx, `update matter_codex_agent_delegations set title = $1 where target_session_id is not null`, safeTitle); err != nil {
		t.Fatalf("исправление title: %v", err)
	}
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Допустимый callback.")
	if err != nil || strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatalf("corrected retry result=%#v error=%v", result, err)
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
	if fixture.publisher.posts.Load() != 2 {
		t.Fatalf("corrected retry posts=%d", fixture.publisher.posts.Load())
	}
}

func TestStartAgentThreadPostgresRejectsLongTitleBeforeDurableEffects(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	counts := func() [4]int64 {
		var result [4]int64
		if err := fixture.pool.QueryRow(ctx, `
select
	(select count(*) from matter_codex_agent_delegations),
	(select count(*) from matter_codex_agent_session_turns),
	(select count(*) from matter_codex_agent_runs),
	(select count(*) from matter_codex_audit_events)
`).Scan(&result[0], &result[1], &result[2], &result[3]); err != nil {
			t.Fatalf("подсчёт durable rows: %v", err)
		}
		return result
	}
	before := counts()
	command := StartAgentThreadCommand{
		TargetChat: "target", TargetAgent: "worker-proof",
		Title: strings.Repeat("я", delegationTitleMaxBytes/2+1), Message: "Допустимое сообщение.",
		WorkItemKey: "issue-71-postgres-title",
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := fixture.service.StartAgentThread(ctx, "source-session", "source-token", command); err == nil || !strings.Contains(err.Error(), "title exceeds") {
			t.Fatalf("attempt %d error = %v", attempt+1, err)
		}
	}
	if after := counts(); after != before || fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("invalid start effects before=%v after=%v network_attempts=%d posts=%d", before, after, fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterProductionDispatcherRevocationFailsBeforeEffects(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := fixture.pool.Exec(ctx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'source-session', 'synthetic test revocation')
`); err != nil {
		t.Fatalf("revoke frozen source: %v", err)
	}
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов."); err == nil {
		t.Fatal("ReturnToRequester() accepted revoked frozen source")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	if fixture.publisher.posts.Load() != 0 {
		t.Fatal("revoked callback published a Mattermost effect")
	}
}

func TestReturnToRequesterProductionDispatcherPoolOneConcurrentIdempotency(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	runIDs := make(chan string, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический конкурентный результат.")
			if err == nil && (result.TargetChat != "target" || result.TargetAgent != "worker-proof") {
				err = fmt.Errorf("concurrent callback returned incomplete target metadata")
			}
			if err == nil {
				runIDs <- result.CallbackRunID
			}
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(runIDs)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent ReturnToRequester() error: %v", err)
		}
	}
	var callbackRunID string
	for runID := range runIDs {
		if strings.TrimSpace(runID) == "" {
			t.Fatal("concurrent callback returned an empty run id")
		}
		if callbackRunID == "" {
			callbackRunID = runID
		} else if callbackRunID != runID {
			t.Fatal("concurrent callback produced multiple run identities")
		}
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
	if fixture.publisher.posts.Load() != 2 || fixture.publisher.attempts.Load() != 2 {
		t.Fatalf("concurrent callback audit posts=%d attempts=%d, want exactly two", fixture.publisher.posts.Load(), fixture.publisher.attempts.Load())
	}
}

func TestReturnToRequesterProductionDispatcherErrorRollsBackAndRetriesOnce(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dispatcher := &failOnceTransactionalDispatcher{AgentTurnDispatcher: fixture.dispatcher, delegate: fixture.dispatcher}
	fixture.service.cfg.TurnDispatcher = dispatcher
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат с повтором."); err == nil {
		t.Fatal("ReturnToRequester() accepted a failed transactional prepare")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат с повтором.")
	if err != nil || strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatalf("ReturnToRequester() retry result=%#v error=%v", result, err)
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
}

func TestReturnToRequesterPostgresFaultsRollbackEveryPersistenceBoundary(t *testing.T) {
	stages := []string{"after_enqueue", "after_callback_run", "after_first_delivery", "after_second_delivery", "on_commit"}
	for _, sourceAccess := range []string{"read-only", "cluster-admin"} {
		for _, stage := range stages {
			t.Run(sourceAccess+"/"+stage, func(t *testing.T) {
				fixture := newPostgresDelegationFixtureWithSourceAccess(t, 1, sourceAccess)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				removeFault := installPostgresCallbackPersistenceFault(t, ctx, fixture.pool, stage)
				if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетическая атомарная ошибка."); err == nil {
					t.Fatalf("%s/%s fault не остановил callback", sourceAccess, stage)
				}
				assertPostgresCallbackPersistenceEmpty(t, ctx, fixture)
				removeFault()
				result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Исправленный атомарный повтор.")
				if err != nil || strings.TrimSpace(result.CallbackRunID) == "" {
					t.Fatalf("%s/%s retry result=%#v error=%v", sourceAccess, stage, result, err)
				}
				assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
				assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
			})
		}
	}
}

func TestReturnToRequesterFinalPublishGuardRejectsCommittedRevocationBeforeNetwork(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	baseStore := fixture.service.cfg.Store.(*postgresrepo.Repository)
	hooked := &exactGuardHookStore{Repository: baseStore, beforeAt: 2}
	hooked.before = func() {
		if _, err := fixture.pool.Exec(ctx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'source-session', 'synthetic final-boundary revocation')
`); err != nil {
			t.Fatalf("commit source revocation before final guard: %v", err)
		}
	}
	fixture.service.cfg.Store = hooked
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов."); err == nil {
		t.Fatal("ReturnToRequester() published after a committed pre-boundary revocation")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
	if fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("committed pre-boundary revocation produced attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterFinalPublishGuardRejectsCommittedChildRemapBeforeNetwork(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	baseStore := fixture.service.cfg.Store.(*postgresrepo.Repository)
	hooked := &exactGuardHookStore{Repository: baseStore, beforeAt: 2}
	hooked.before = func() {
		if _, err := fixture.pool.Exec(ctx, `
update matter_codex_chat_participants participant
set enabled = false
from matter_codex_agent_sessions session_row
where session_row.session_key = 'target-session'
	and participant.chat_id = session_row.chat_id
	and participant.role_id = session_row.role_id
`); err != nil {
			t.Fatalf("commit child remap before final guard: %v", err)
		}
	}
	fixture.service.cfg.Store = hooked
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов."); err == nil {
		t.Fatal("ReturnToRequester() published after a committed pre-boundary child remap")
	}
	assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
	if fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("committed pre-boundary child remap produced attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterFinalPublishGuardSerializesRevocationStartedAfterLocks(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	revokeDone := make(chan error, 1)
	fixture.publisher.beforeFirst = func() {
		go func() {
			_, err := fixture.pool.Exec(ctx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'source-session', 'synthetic concurrent final-boundary revocation')
`)
			revokeDone <- err
		}()
		select {
		case err := <-revokeDone:
			t.Fatalf("source revocation completed before the guarded network call returned: %v", err)
		case <-time.After(150 * time.Millisecond):
		}
	}
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов."); err == nil {
		t.Fatal("second audit boundary unexpectedly survived the serialized source revocation")
	}
	select {
	case err := <-revokeDone:
		if err != nil {
			t.Fatalf("serialized source revocation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("source revocation remained blocked after guarded publish returned")
	}
	if fixture.publisher.posts.Load() != 1 {
		t.Fatalf("serialized publish posts = %d, want the first post linearized before revocation", fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterAuditNetworkErrorIsPartialAndRetryDoesNotDuplicate(t *testing.T) {
	for _, failAt := range []int64{1, 2} {
		t.Run("failure_"+strconv.FormatInt(failAt, 10), func(t *testing.T) {
			fixture := newPostgresDelegationFixture(t, 1)
			fixture.publisher.failAt.Store(failAt)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический результат готов.")
			if err == nil || strings.TrimSpace(result.CallbackRunID) == "" {
				t.Fatalf("ReturnToRequester() result=%#v error=%v", result, err)
			}
			assertPostgresDelegationCallbackState(t, ctx, fixture, 1)
			if fixture.publisher.attempts.Load() != 2 || fixture.publisher.posts.Load() != 1 {
				t.Fatalf("partial network outcome attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
			}
			assertCallbackDeliveryCounts(t, ctx, fixture, 1, 1)
			fixture.publisher.failAt.Store(0)
			if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Повторный результат."); err != nil {
				t.Fatalf("idempotent retry: %v", err)
			}
			if fixture.publisher.attempts.Load() != 3 || fixture.publisher.posts.Load() != 2 {
				t.Fatalf("retry duplicated audit attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
			}
			assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
		})
	}
}

func TestReturnToRequesterDoubleFailureIsDurableAndCorrectedByRetry(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 1)
	fixture.publisher.failThrough.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический двойной отказ.")
	if err == nil || strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatalf("двойной отказ не оставил durable callback: result=%#v error=%v", result, err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 0, 2)
	if fixture.publisher.attempts.Load() != 2 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("двойной отказ attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}

	fixture.publisher.failThrough.Store(0)
	restarted := NewAgentSessionService(fixture.service.cfg)
	if _, err := restarted.ReturnToRequester(ctx, "target-session", "target-token", "Повтор после перезапуска."); err != nil {
		t.Fatalf("повтор после двойного отказа: %v", err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
	if fixture.publisher.attempts.Load() != 4 || fixture.publisher.posts.Load() != 2 || len(fixture.publisher.deliveries) != 2 {
		t.Fatalf("исправленный повтор attempts=%d posts=%d external_ids=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load(), len(fixture.publisher.deliveries))
	}
}

func TestReturnToRequesterNetworkSuccessDatabaseMarkFailureReconcilesAfterRestart(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	baseStore := fixture.service.cfg.Store.(*postgresrepo.Repository)
	failingStore := &failOnceDeliveryStore{Repository: baseStore}
	fixture.service.cfg.Store = failingStore
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетическая неоднозначность подтверждения.")
	if err == nil || strings.TrimSpace(result.CallbackRunID) == "" || !failingStore.failed.Load() {
		t.Fatalf("ошибка DB mark не осталась явной: result=%#v error=%v", result, err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 1, 1)
	if fixture.publisher.attempts.Load() != 2 || fixture.publisher.posts.Load() != 2 {
		t.Fatalf("первичная доставка attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
	if _, err := fixture.pool.Exec(ctx, `
update matter_codex_agent_delegation_callback_deliveries
set lease_expires_at = now() - interval '1 second'
where status = 'in_flight'
`); err != nil {
		t.Fatalf("истечение synthetic lease: %v", err)
	}

	config := fixture.service.cfg
	config.Store = baseStore
	restarted := NewAgentSessionService(config)
	if _, err := restarted.ReturnToRequester(ctx, "target-session", "target-token", "Повтор после неоднозначности."); err != nil {
		t.Fatalf("reconcile после перезапуска: %v", err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
	if fixture.publisher.attempts.Load() != 2 || fixture.publisher.posts.Load() != 2 || len(fixture.publisher.deliveries) != 2 {
		t.Fatalf("reconcile создал дубликат attempts=%d posts=%d external_ids=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load(), len(fixture.publisher.deliveries))
	}
}

func TestReturnToRequesterConcurrentRetriesClaimEachPublicationOnce(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 4)
	fixture.publisher.failThrough.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический первичный отказ."); err == nil {
		t.Fatal("первичный двойной отказ неожиданно успешен")
	}
	fixture.publisher.failThrough.Store(0)
	fixture.publisher.deliveryDelay = 50 * time.Millisecond

	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, retryErr := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Конкурентный повтор.")
			results <- retryErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for range results {
		// Пока другой caller удерживает lease, явная incomplete error допустима.
	}
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Финальная сверка."); err != nil {
		t.Fatalf("финальная сверка после конкурентных повторов: %v", err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
	if fixture.publisher.concurrentDuplicate.Load() || fixture.publisher.attempts.Load() != 4 || fixture.publisher.posts.Load() != 2 || len(fixture.publisher.deliveries) != 2 {
		t.Fatalf("конкурентные повторы duplicate=%t attempts=%d posts=%d external_ids=%d", fixture.publisher.concurrentDuplicate.Load(), fixture.publisher.attempts.Load(), fixture.publisher.posts.Load(), len(fixture.publisher.deliveries))
	}
}

func TestReturnToRequesterHungPublicationsRemainPendingUntilCorrectedRetry(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	fixture.service.cfg.CallbackPublishDeadline = 100 * time.Millisecond
	fixture.publisher.blockUntilContext.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетические зависшие публикации.")
	if err == nil || strings.TrimSpace(result.CallbackRunID) == "" {
		t.Fatalf("hung outcome не остался явным: result=%#v error=%v", result, err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 0, 2)
	if fixture.publisher.attempts.Load() != 2 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("hung outcome attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
	fixture.publisher.blockUntilContext.Store(false)
	if _, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Исправленный повтор."); err != nil {
		t.Fatalf("исправленный повтор после hung outcome: %v", err)
	}
	assertCallbackDeliveryCounts(t, ctx, fixture, 2, 0)
	if fixture.publisher.attempts.Load() != 4 || fixture.publisher.posts.Load() != 2 {
		t.Fatalf("исправленный hung retry attempts=%d posts=%d", fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func TestReturnToRequesterHungPublishHasScopedBoundedFence(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 4)
	fixture.service.cfg.CallbackPublishDeadline = 350 * time.Millisecond
	fixture.publisher.blockUntilContext.Store(true)
	fixture.publisher.started = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fixture.pool.Exec(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, status, ttl_seconds, expires_at
)
select 'unrelated-session', project_id, chat_id, role_id, 'thread_role',
	mattermost_channel_id, 'unrelated-root', 'idle', 3600, now() + interval '1 hour'
from matter_codex_agent_sessions where session_key = 'target-session'
`); err != nil {
		t.Fatalf("seed unrelated session: %v", err)
	}
	callbackDone := make(chan error, 1)
	startedAt := time.Now()
	go func() {
		_, err := fixture.service.ReturnToRequester(ctx, "target-session", "target-token", "Синтетический hung publish.")
		callbackDone <- err
	}()
	select {
	case <-fixture.publisher.started:
	case <-ctx.Done():
		t.Fatal("publisher не достигнут до общего deadline")
	}
	sameRevokeDone := make(chan error, 1)
	go func() {
		_, err := fixture.pool.Exec(ctx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'source-session', 'synthetic same-subject deadline revocation')
`)
		sameRevokeDone <- err
	}()
	select {
	case err := <-sameRevokeDone:
		t.Fatalf("same-subject revoke обошёл delivery fence: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unrelatedCtx, cancelUnrelated := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancelUnrelated()
	if _, err := fixture.pool.Exec(unrelatedCtx, `
insert into matter_codex_cluster_admin_revocations(resource_type, resource_key, reason)
values ('session_key', 'unrelated-session', 'synthetic unrelated deadline revocation')
`); err != nil {
		t.Fatalf("unrelated revoke был заблокирован hung publish: %v", err)
	}
	select {
	case err := <-callbackDone:
		if err == nil {
			t.Fatal("hung publish неожиданно завершился успешно")
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("server-owned publish deadline не ограничил boundary: %s", elapsed)
		}
	case <-ctx.Done():
		t.Fatal("callback boundary не завершился по server-owned deadline")
	}
	select {
	case err := <-sameRevokeDone:
		if err != nil {
			t.Fatalf("same-subject revoke после deadline: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("same-subject revoke остался заблокирован после deadline")
	}
	var one int
	if err := fixture.pool.QueryRow(ctx, `select 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("pgx pool не освобождён после deadline: value=%d error=%v", one, err)
	}
	if fixture.publisher.posts.Load() != 0 || fixture.publisher.attempts.Load() != 1 {
		t.Fatalf("hung publisher posts=%d attempts=%d", fixture.publisher.posts.Load(), fixture.publisher.attempts.Load())
	}
}

func TestExactPublishGuardTwoConnectionLockOrderHasNoDeadlock(t *testing.T) {
	fixture := newPostgresDelegationFixture(t, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sourceSession, err := fixture.service.cfg.Store.GetAgentSession(ctx, "source-session")
	if err != nil {
		t.Fatalf("read source session: %v", err)
	}
	childSession, err := fixture.service.cfg.Store.GetAgentSession(ctx, "target-session")
	if err != nil {
		t.Fatalf("read child session: %v", err)
	}
	writer, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin writer: %v", err)
	}
	defer func() { _ = writer.Rollback(context.Background()) }()
	if _, err := writer.Exec(ctx, `
select identity.id
from matter_codex_mattermost_bot_identities identity
where identity.role_id = $1
for update
`, sourceSession.RoleID); err != nil {
		t.Fatalf("lock dependency subject row: %v", err)
	}
	guardDone := make(chan error, 1)
	var networkCalls atomic.Int64
	go func() {
		guardDone <- fixture.service.withCurrentSessionsPublishGuard(ctx, childSession, sourceSession, "synthetic.lock_order", func(entity.AgentSession, entity.AgentSession) error {
			networkCalls.Add(1)
			return nil
		})
	}()
	select {
	case err := <-guardDone:
		t.Fatalf("guard не дождался уже захваченной dependency row: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := writer.Exec(ctx, `
delete from matter_codex_mattermost_bot_identities
where role_id = $1
`, sourceSession.RoleID); err != nil {
		t.Fatalf("dependency-trigger writer revocation встретил deadlock/timeout: %v", err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit dependency writer: %v", err)
	}
	select {
	case err := <-guardDone:
		if err == nil {
			t.Fatal("guard не увидел committed dependency revocation")
		}
	case <-ctx.Done():
		t.Fatal("guard остался заблокирован после writer commit")
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("network calls after committed dependency revocation = %d", networkCalls.Load())
	}
}

type postgresDelegationFixture struct {
	pool       *pgxpool.Pool
	service    *AgentSessionService
	dispatcher *ChatRunService
	publisher  *postgresDelegationPublisher
}

type failOnceTransactionalDispatcher struct {
	AgentTurnDispatcher
	delegate TransactionalAgentTurnDispatcher
	failed   atomic.Bool
}

func (dispatcher *failOnceTransactionalDispatcher) EnqueueExistingAgentTurn(ctx context.Context, store adminrepo.Repository, session entity.AgentSession, request AgentTurnRequest) (AgentTurnQueued, error) {
	queued, err := dispatcher.delegate.EnqueueExistingAgentTurn(ctx, store, session, request)
	if err != nil {
		return AgentTurnQueued{}, err
	}
	if !dispatcher.failed.Swap(true) {
		return AgentTurnQueued{}, fmt.Errorf("synthetic transactional prepare failure")
	}
	return queued, nil
}

type postgresDelegationRunner struct {
	runtimerepo.Runner
	tokenReads     atomic.Int64
	integrityReads atomic.Int64
}

func (runner *postgresDelegationRunner) GetMattermostBotTokenSecret(_ context.Context, secretName string) (runtimerepo.MattermostBotTokenSecret, error) {
	runner.tokenReads.Add(1)
	token := ""
	switch secretName {
	case "source-secret":
		token = "source-token"
	case "target-secret":
		token = "target-token"
	default:
		return runtimerepo.MattermostBotTokenSecret{}, fmt.Errorf("synthetic token secret not found")
	}
	return runtimerepo.MattermostBotTokenSecret{
		SecretName: secretName, Token: token,
		Integrity: runtimerepo.SecretIntegrity{SecretName: secretName, SecretKey: "token", ContentSHA256: strings.Repeat("a", 64), UID: "source-session-uid", ResourceVersion: "1"},
	}, nil
}

func (runner *postgresDelegationRunner) InspectSecretIntegrity(_ context.Context, input runtimerepo.SecretIntegrityInput) (runtimerepo.SecretIntegrity, error) {
	runner.integrityReads.Add(1)
	switch input.SecretName + "/" + input.SecretKey {
	case "matter-codex-codex-auth-primary/auth.json":
		return runtimerepo.SecretIntegrity{SecretName: input.SecretName, SecretKey: input.SecretKey, ContentSHA256: strings.Repeat("c", 64), UID: "source-codex-auth-uid", ResourceVersion: "1"}, nil
	case "source-bot-secret/token":
		return runtimerepo.SecretIntegrity{SecretName: input.SecretName, SecretKey: input.SecretKey, ContentSHA256: strings.Repeat("b", 64), UID: "source-bot-uid", ResourceVersion: "1"}, nil
	case "source-secret/token":
		return runtimerepo.SecretIntegrity{SecretName: input.SecretName, SecretKey: input.SecretKey, ContentSHA256: strings.Repeat("a", 64), UID: "source-session-uid", ResourceVersion: "1"}, nil
	default:
		return runtimerepo.SecretIntegrity{}, fmt.Errorf("synthetic integrity binding not found")
	}
}

type postgresDelegationPublisher struct {
	MattermostThreadPublisher
	attempts            atomic.Int64
	posts               atomic.Int64
	failAt              atomic.Int64
	failThrough         atomic.Int64
	beforeOnce          sync.Once
	beforeFirst         func()
	blockUntilContext   atomic.Bool
	concurrentDuplicate atomic.Bool
	started             chan struct{}
	startedOnce         sync.Once
	deliveryMu          sync.Mutex
	deliveries          map[string]MattermostPostRef
	activeDeliveries    map[string]bool
	deliveryDelay       time.Duration
}

func (publisher *postgresDelegationPublisher) PostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	attempt := publisher.attempts.Add(1)
	publisher.beforeOnce.Do(func() {
		if publisher.beforeFirst != nil {
			publisher.beforeFirst()
		}
	})
	if publisher.blockUntilContext.Load() {
		publisher.startedOnce.Do(func() {
			if publisher.started != nil {
				close(publisher.started)
			}
		})
		<-ctx.Done()
		return MattermostPostRef{}, ctx.Err()
	}
	if publisher.failAt.Load() == attempt {
		return MattermostPostRef{}, fmt.Errorf("synthetic Mattermost network failure")
	}
	if maximum := publisher.failThrough.Load(); maximum > 0 && attempt <= maximum {
		return MattermostPostRef{}, fmt.Errorf("synthetic Mattermost network failure")
	}
	id := publisher.posts.Add(1)
	return MattermostPostRef{ChannelID: input.ChannelID, PostID: "synthetic-post-" + strconv.FormatInt(id, 10)}, nil
}

func (publisher *postgresDelegationPublisher) PostThreadMessageWithToken(ctx context.Context, _ string, input MattermostThreadPostInput) (MattermostPostRef, error) {
	return publisher.PostThreadMessage(ctx, input)
}

func (publisher *postgresDelegationPublisher) PostThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: "synthetic-card-" + card.RootPostID}, nil
}

func (publisher *postgresDelegationPublisher) UpdateThreadCard(_ context.Context, card MattermostCard) (MattermostPostRef, error) {
	return MattermostPostRef{ChannelID: card.ChannelID, PostID: card.PostID}, nil
}

func (publisher *postgresDelegationPublisher) ReconcileOrPostThreadMessage(ctx context.Context, input MattermostThreadPostInput) (MattermostPostRef, error) {
	publisher.deliveryMu.Lock()
	if ref, ok := publisher.deliveries[input.IdempotencyID]; ok {
		publisher.deliveryMu.Unlock()
		return ref, nil
	}
	if publisher.activeDeliveries == nil {
		publisher.activeDeliveries = map[string]bool{}
	}
	if publisher.activeDeliveries[input.IdempotencyID] {
		publisher.concurrentDuplicate.Store(true)
		publisher.deliveryMu.Unlock()
		return MattermostPostRef{}, fmt.Errorf("synthetic parallel duplicate delivery")
	}
	publisher.activeDeliveries[input.IdempotencyID] = true
	publisher.deliveryMu.Unlock()
	defer func() {
		publisher.deliveryMu.Lock()
		delete(publisher.activeDeliveries, input.IdempotencyID)
		publisher.deliveryMu.Unlock()
	}()
	if publisher.deliveryDelay > 0 {
		timer := time.NewTimer(publisher.deliveryDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return MattermostPostRef{}, ctx.Err()
		}
	}
	ref, err := publisher.PostThreadMessage(ctx, input)
	if err != nil {
		return MattermostPostRef{}, err
	}
	publisher.deliveryMu.Lock()
	if publisher.deliveries == nil {
		publisher.deliveries = map[string]MattermostPostRef{}
	}
	if existing, ok := publisher.deliveries[input.IdempotencyID]; ok {
		ref = existing
	} else {
		publisher.deliveries[input.IdempotencyID] = ref
	}
	publisher.deliveryMu.Unlock()
	return ref, nil
}

type exactGuardHookStore struct {
	*postgresrepo.Repository
	beforeAt int64
	calls    atomic.Int64
	before   func()
}

type failOnceDeliveryStore struct {
	*postgresrepo.Repository
	failed atomic.Bool
}

func (store *failOnceDeliveryStore) DeliverAgentDelegationCallbackDelivery(ctx context.Context, input adminrepo.DeliverAgentDelegationCallbackDeliveryInput) (entity.AgentDelegationCallbackDelivery, error) {
	if store.failed.CompareAndSwap(false, true) {
		return entity.AgentDelegationCallbackDelivery{}, fmt.Errorf("synthetic callback delivery mark failure")
	}
	return store.Repository.DeliverAgentDelegationCallbackDelivery(ctx, input)
}

func (store *exactGuardHookStore) WithExactAgentSessionsRuntimeGuard(ctx context.Context, expected []entity.AgentSession, sideEffect func(adminrepo.Repository) error) error {
	if store.calls.Add(1) == store.beforeAt && store.before != nil {
		store.before()
	}
	return store.Repository.WithExactAgentSessionsRuntimeGuard(ctx, expected, sideEffect)
}

func newPostgresDelegationFixture(t *testing.T, maxConnections int32) postgresDelegationFixture {
	return newPostgresDelegationFixtureWithSourceAccess(t, maxConnections, "cluster-admin")
}

func newPostgresDelegationFixtureWithSourceAccess(t *testing.T, maxConnections int32, sourceAccess string) postgresDelegationFixture {
	t.Helper()
	dsn := isolatedDelegationPostgresDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 21); err != nil {
		t.Fatalf("migrations through v21: %v", err)
	}
	seedPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open delegation seed pool: %v", err)
	}
	seedPostgresDelegation(t, ctx, seedPool, sourceAccess)
	seedPool.Close()
	if err := migrations.RunTo(ctx, dsn, 24); err != nil {
		t.Fatalf("migrations through v24: %v", err)
	}
	seedPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("reopen delegation seed pool: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
update matter_codex_agent_sessions
set secret_content_sha256 = repeat('a', 64), secret_resource_uid = 'source-session-uid', secret_resource_version = '1'
where session_key = 'source-session'
`); err != nil {
		seedPool.Close()
		t.Fatalf("stage session Secret integrity metadata: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
update matter_codex_mattermost_bot_identities
set secret_content_sha256 = repeat('b', 64), secret_resource_uid = 'source-bot-uid', secret_resource_version = '1'
where token_secret_ref = 'source-bot-secret'
`); err != nil {
		seedPool.Close()
		t.Fatalf("stage bot Secret integrity metadata: %v", err)
	}
	if _, err := seedPool.Exec(ctx, `
update matter_codex_credentials credential
set secret_content_sha256 = repeat('c', 64), secret_resource_uid = 'source-codex-auth-uid', secret_resource_version = '1'
from matter_codex_openai_accounts account
where account.name = 'primary' and account.credential_id = credential.id
`); err != nil {
		seedPool.Close()
		t.Fatalf("stage OpenAI Secret integrity metadata: %v", err)
	}
	seedPool.Close()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrations through v31: %v", err)
	}
	seedPool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open process seed pool: %v", err)
	}
	seedPostgresDelegationProcess(t, ctx, seedPool)
	seedPool.Close()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse delegation pool config: %v", err)
	}
	config.MaxConns = maxConnections
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set lock_timeout = '750ms'; set statement_timeout = '8s'`)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open bounded delegation pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository := postgresrepo.NewRepository(pool)
	runner := &postgresDelegationRunner{}
	publisher := &postgresDelegationPublisher{}
	chatRuns := NewChatRunService(ChatRunServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: publisher,
		StorageReady: true, RuntimeReady: true, DisableMonitor: true,
	})
	service := NewAgentSessionService(AgentSessionServiceConfig{
		Store: repository, RuntimeRunner: runner, ThreadPublisher: publisher, TurnDispatcher: chatRuns,
		MattermostSiteURL: "https://mattermost.example", StorageReady: true, RuntimeReady: true,
		CallbackPublishConcurrency: 32,
	})
	return postgresDelegationFixture{pool: pool, service: service, dispatcher: chatRuns, publisher: publisher}
}

func seedPostgresDelegationProcess(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var projectID, policyID, sourceRoleID, sourceTurnID, targetTurnID int64
	if err := pool.QueryRow(ctx, `
select source_session.project_id, policy.id, source_session.role_id, source_turn.id, target_turn.id
from matter_codex_agent_sessions source_session
join matter_codex_agent_session_turns source_turn on source_turn.session_id = source_session.id and source_turn.run_id = 'source-run'
join matter_codex_agent_sessions target_session on target_session.session_key = 'target-session'
join matter_codex_agent_session_turns target_turn on target_turn.session_id = target_session.id and target_turn.run_id = 'target-run'
join matter_codex_policy_revisions policy on policy.project_id = source_session.project_id and policy.status = 'active'
where source_session.session_key = 'source-session'
`).Scan(&projectID, &policyID, &sourceRoleID, &sourceTurnID, &targetTurnID); err != nil {
		t.Fatalf("read delegation process identities: %v", err)
	}
	if _, err := pool.Exec(ctx, `
update matter_codex_agent_session_turns
set user_id = case id when $1 then 'owner-user' else 'worker-user' end,
	user_name = case id when $1 then 'owner' else 'worker-proof' end
where id in ($1, $2)
`, sourceTurnID, targetTurnID); err != nil {
		t.Fatalf("seed delegation turn actors: %v", err)
	}
	var processID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_process_runs(
	public_id, project_id, policy_revision_id, root_role_id, root_initiator_user_id,
	root_initiator_user_name, root_trigger_post_id, root_channel_id, root_thread_post_id, status
) values ('delegation-proof-process', $1, $2, $3, 'owner-user', 'owner', 'source-root', 'source-channel', 'source-root', 'running')
returning id
`, projectID, policyID, sourceRoleID).Scan(&processID); err != nil {
		t.Fatalf("seed delegation process: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_process_turns(turn_id, process_run_id, parent_turn_id, launch_post_id)
values ($1, $3, null, 'source-root'), ($2, $3, $1, 'target-root')
`, sourceTurnID, targetTurnID, processID); err != nil {
		t.Fatalf("seed delegation process turns: %v", err)
	}
}

func seedPostgresDelegation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sourceAccess string) {
	t.Helper()
	var projectID, sourceRoleID, targetRoleID, sourceChatID, targetChatID int64
	if _, err := pool.Exec(ctx, `update matter_codex_openai_accounts set status = 'authorized' where name = 'primary'`); err != nil {
		t.Fatalf("authorize delegation OpenAI account: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Delegation proof', 'delegation-proof') returning id`).Scan(&projectID); err != nil {
		t.Fatalf("seed delegation project: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, openai_account_name, kubernetes_access) values ($1, 'manager-proof', 'manager', 'primary', $2) returning id`, projectID, sourceAccess).Scan(&sourceRoleID); err != nil {
		t.Fatalf("seed source role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, openai_account_name, kubernetes_access) values ($1, 'worker-proof', 'worker', 'primary', 'read-only') returning id`, projectID).Scan(&targetRoleID); err != nil {
		t.Fatalf("seed target role: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, chat_type) values ($1, 'source-channel', 'Source', 'source', 'manager') returning id`, projectID).Scan(&sourceChatID); err != nil {
		t.Fatalf("seed source chat: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, chat_type) values ($1, 'target-channel', 'Target', 'target', 'custom') returning id`, projectID).Scan(&targetChatID); err != nil {
		t.Fatalf("seed target chat: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into matter_codex_chat_participants(chat_id, role_id) values ($1, $2), ($3, $4)`, sourceChatID, sourceRoleID, targetChatID, targetRoleID); err != nil {
		t.Fatalf("seed participants: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_mattermost_bot_identities(project_id, role_id, username, mattermost_user_id, token_secret_ref, status)
values ($1, $2, 'manager-proof', 'manager-user', 'source-bot-secret', 'active'),
	($1, $3, 'worker-proof', 'worker-user', 'target-bot-secret', 'active')
`, projectID, sourceRoleID, targetRoleID); err != nil {
		t.Fatalf("seed bot identities: %v", err)
	}
	var sourceSessionID, targetSessionID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, openai_account_name, status, kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
) values ('source-session', $1, $2, $3, 'thread_role', 'source-channel', 'source-root', 'primary', 'running', 'mattermost', 'source-pod', 'source-pvc', 'source-secret', 3600, now() + interval '1 hour')
returning id
`, projectID, sourceChatID, sourceRoleID).Scan(&sourceSessionID); err != nil {
		t.Fatalf("seed source session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_sessions(
	session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
	mattermost_root_post_id, openai_account_name, status, kubernetes_namespace, pod_name, pvc_name, token_secret_ref, ttl_seconds, expires_at
) values ('target-session', $1, $2, $3, 'thread_role', 'target-channel', 'target-root', 'primary', 'running', 'mattermost', 'target-pod', 'target-pvc', 'target-secret', 3600, now() + interval '1 hour')
returning id
`, projectID, targetChatID, targetRoleID).Scan(&targetSessionID); err != nil {
		t.Fatalf("seed target session: %v", err)
	}
	var sourceTurnID, targetTurnID int64
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status)
values ($1, 'source-run', 'source-channel', 'source-root', 'source-root', 'source work', 'running') returning id
`, sourceSessionID).Scan(&sourceTurnID); err != nil {
		t.Fatalf("seed source turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `
insert into matter_codex_agent_session_turns(session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status)
values ($1, 'target-run', 'target-channel', 'target-root', 'target-root', 'target work', 'running') returning id
`, targetSessionID).Scan(&targetTurnID); err != nil {
		t.Fatalf("seed target turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $1, active_run_id = 'source-run' where id = $2`, sourceTurnID, sourceSessionID); err != nil {
		t.Fatalf("bind source active turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $1, active_run_id = 'target-run' where id = $2`, targetTurnID, targetSessionID); err != nil {
		t.Fatalf("bind target active turn: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_agent_delegations(
	project_id, source_session_id, source_turn_id, target_chat_id, target_role_id,
	target_root_post_id, target_session_id, target_turn_id, target_run_id, work_item_key, title, status
) values ($1, $2, $3, $4, $5, 'target-root', $6, $7, 'target-run', 'transaction-proof', 'Транзакционный callback', 'running')
`, projectID, sourceSessionID, sourceTurnID, targetChatID, targetRoleID, targetSessionID, targetTurnID); err != nil {
		t.Fatalf("seed delegation: %v", err)
	}
}

func postgresDelegationArchiveFixture(value string) (string, string, int64) {
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions", Typeflag: tar.TypeDir, Mode: 0o700, Format: tar.FormatUSTAR}); err != nil {
		panic(err)
	}
	body := []byte(value)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions/state.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body)), Format: tar.FormatUSTAR}); err != nil {
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

func assertPostgresDelegationCallbackState(t *testing.T, ctx context.Context, fixture postgresDelegationFixture, expectedCallbacks int) {
	t.Helper()
	var callbackRows, queuedTurns, callbackRuns int
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_agent_delegations where callback_turn_id is not null and callback_run_id <> ''`).Scan(&callbackRows); err != nil {
		t.Fatalf("count persisted callbacks: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `
select count(*) from matter_codex_agent_session_turns turn_row
join matter_codex_agent_sessions session_row on session_row.id = turn_row.session_id
where session_row.session_key = 'source-session' and turn_row.status = 'queued'
`).Scan(&queuedTurns); err != nil {
		t.Fatalf("count callback turns: %v", err)
	}
	if err := fixture.pool.QueryRow(ctx, `select count(*) from matter_codex_agent_runs where status = 'queued' and flow_id = 'session-source-session'`).Scan(&callbackRuns); err != nil {
		t.Fatalf("count callback runs: %v", err)
	}
	if callbackRows != expectedCallbacks || queuedTurns != expectedCallbacks || callbackRuns != expectedCallbacks {
		t.Fatalf("callback state rows=%d turns=%d runs=%d, want=%d", callbackRows, queuedTurns, callbackRuns, expectedCallbacks)
	}
	if expectedCallbacks == 1 {
		var userID string
		if err := fixture.pool.QueryRow(ctx, `
select turn_row.user_id
from matter_codex_agent_session_turns turn_row
join matter_codex_agent_sessions session_row on session_row.id = turn_row.session_id
where session_row.session_key = 'source-session' and turn_row.status = 'queued'
`).Scan(&userID); err != nil {
			t.Fatalf("read callback root initiator: %v", err)
		}
		if userID != "owner-user" {
			t.Fatalf("callback user id = %q", userID)
		}
	}
}

func assertCallbackDeliveryCounts(t *testing.T, ctx context.Context, fixture postgresDelegationFixture, delivered int, incomplete int) {
	t.Helper()
	var deliveredRows, incompleteRows int
	if err := fixture.pool.QueryRow(ctx, `
select
	count(*) filter (where status = 'delivered'),
	count(*) filter (where status <> 'delivered')
from matter_codex_agent_delegation_callback_deliveries
`).Scan(&deliveredRows, &incompleteRows); err != nil {
		t.Fatalf("подсчёт callback deliveries: %v", err)
	}
	if deliveredRows != delivered || incompleteRows != incomplete {
		t.Fatalf("callback deliveries delivered=%d incomplete=%d, want %d/%d", deliveredRows, incompleteRows, delivered, incomplete)
	}
}

func assertPostgresCallbackPersistenceEmpty(t *testing.T, ctx context.Context, fixture postgresDelegationFixture) {
	t.Helper()
	assertPostgresDelegationCallbackState(t, ctx, fixture, 0)
	var deliveries, manifests int
	if err := fixture.pool.QueryRow(ctx, `
select
	(select count(*) from matter_codex_agent_delegation_callback_deliveries),
	(select count(*) from matter_codex_agent_delegation_callback_delivery_manifests)
`).Scan(&deliveries, &manifests); err != nil {
		t.Fatalf("count rolled back callback plan: %v", err)
	}
	if deliveries != 0 || manifests != 0 || fixture.publisher.attempts.Load() != 0 || fixture.publisher.posts.Load() != 0 {
		t.Fatalf("partial callback state deliveries=%d manifests=%d attempts=%d posts=%d", deliveries, manifests, fixture.publisher.attempts.Load(), fixture.publisher.posts.Load())
	}
}

func installPostgresCallbackPersistenceFault(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stage string) func() {
	t.Helper()
	functionBody := ""
	table := ""
	timing := "after insert"
	switch stage {
	case "after_enqueue":
		table = "matter_codex_agent_runs"
		functionBody = `
if new.flow_id = 'session-source-session' and new.status = 'queued' then
	raise exception 'synthetic callback fault after enqueue';
end if;`
	case "after_callback_run":
		table = "matter_codex_agent_delegations"
		timing = "after update of callback_run_id"
		functionBody = `
if length(trim(coalesce(old.callback_run_id, ''))) = 0 and length(trim(coalesce(new.callback_run_id, ''))) > 0 then
	raise exception 'synthetic callback fault after callback run';
end if;`
	case "after_first_delivery", "after_second_delivery":
		table = "matter_codex_agent_delegation_callback_deliveries"
		expectedCount := 1
		if stage == "after_second_delivery" {
			expectedCount = 2
		}
		functionBody = fmt.Sprintf(`
if (select count(*) from matter_codex_agent_delegation_callback_deliveries
	where delegation_id = new.delegation_id and callback_run_id = new.callback_run_id) = %d then
	raise exception 'synthetic callback fault after delivery row';
end if;`, expectedCount)
	case "on_commit":
		table = "matter_codex_agent_delegation_callback_delivery_manifests"
		functionBody = `raise exception 'synthetic callback fault on commit';`
	default:
		t.Fatalf("unknown PostgreSQL callback fault stage %q", stage)
	}
	functionName := pgx.Identifier{"matter_codex_test_callback_fault"}.Sanitize()
	triggerName := pgx.Identifier{"matter_codex_test_callback_fault_trigger"}.Sanitize()
	tableName := pgx.Identifier{table}.Sanitize()
	triggerStatement := fmt.Sprintf(
		"create trigger %s %s on %s for each row execute function %s();",
		triggerName, timing, tableName, functionName,
	)
	if stage == "on_commit" {
		triggerStatement = fmt.Sprintf(
			"create constraint trigger %s after insert on %s deferrable initially deferred for each row execute function %s();",
			triggerName, tableName, functionName,
		)
	}
	statement := fmt.Sprintf(`
create function %s() returns trigger language plpgsql as $function$
begin
%s
	return new;
end
$function$;
%s
`, functionName, functionBody, triggerStatement)
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("install PostgreSQL callback fault %s: %v", stage, err)
	}
	return func() {
		t.Helper()
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			"drop trigger %s on %s; drop function %s()",
			triggerName, tableName, functionName,
		)); err != nil {
			t.Fatalf("remove PostgreSQL callback fault %s: %v", stage, err)
		}
	}
}

func proveGuardedSourceUsesSamePostgresTransaction(t *testing.T, ctx context.Context, fixture postgresDelegationFixture) {
	t.Helper()
	source, err := fixture.service.cfg.Store.GetAgentSession(ctx, "source-session")
	if err != nil {
		t.Fatalf("read source session for transaction proof: %v", err)
	}
	repository, ok := fixture.service.cfg.Store.(securityrepo.ClusterAdminPersistenceGuardRepository)
	if !ok {
		t.Fatal("production repository does not expose persistence guard")
	}
	err = repository.WithExistingClusterAdminPersistenceGuard(ctx, securityrepo.ClusterAdminBindingInput{
		RoleID: source.RoleID, ProjectID: source.ProjectID, ChatID: source.ChatID,
		MattermostChannelID: source.MattermostChannelID, SessionKey: source.SessionKey,
		Operation: "test.callback.transaction", ActorUser: "test",
	}, func(guardedStore adminrepo.Repository) error {
		input := adminrepo.UpdateAgentSessionRuntimeInput{SessionKey: source.SessionKey, ExtendTTLSeconds: source.TTLSeconds}
		if _, guardedErr := guardedStore.UpdateAgentSessionRuntime(ctx, input); guardedErr != nil {
			return fmt.Errorf("guarded source update did not use the current transaction: %w", guardedErr)
		}
		outsideResult := make(chan error, 1)
		go func() {
			_, outsideErr := fixture.service.cfg.Store.UpdateAgentSessionRuntime(ctx, input)
			outsideResult <- outsideErr
		}()
		select {
		case outsideErr := <-outsideResult:
			if outsideErr == nil {
				return fmt.Errorf("base pool updated a source row locked by the guarded transaction")
			}
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("two-connection guarded repository proof: %v", err)
	}
}

func isolatedDelegationPostgresDSN(t *testing.T) string {
	t.Helper()
	return testsupport.IsolatedSchemaDSN(t, "callback")
}
