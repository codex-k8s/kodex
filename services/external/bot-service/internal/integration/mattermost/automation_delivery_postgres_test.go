//go:build postgres

package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	automationsrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/automations"
	statusservice "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	adminpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	automationspostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/automations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
	mattermostmodel "github.com/mattermost/mattermost/server/public/model"
)

func TestAutomationDeliveryPostgreSQLFenceSurvivesPostWriteFailureRestartAndLateVisibility(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "automation_delivery_restart")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("применить миграции: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("открыть PostgreSQL pool: %v", err)
	}
	defer func() { pool.Close() }()

	start := time.Date(2026, time.July, 22, 15, 0, 0, 0, time.UTC)
	repository, run := preparePostgreSQLAutomationOwnerGate(t, ctx, pool, start)

	var postCalls atomic.Int64
	var visible atomic.Bool
	var createdMu sync.Mutex
	var created *mattermostmodel.Post
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/posts/automation-restart-root/thread"):
			postList := &mattermostmodel.PostList{Posts: map[string]*mattermostmodel.Post{}, Order: []string{}}
			createdMu.Lock()
			if visible.Load() && created != nil {
				postList.Posts[created.Id] = created.Clone()
				postList.Order = append(postList.Order, created.Id)
			}
			createdMu.Unlock()
			if err := json.NewEncoder(writer).Encode(postList); err != nil {
				t.Errorf("закодировать Mattermost thread: %v", err)
			}
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/posts"):
			postCalls.Add(1)
			var post mattermostmodel.Post
			if err := json.NewDecoder(request.Body).Decode(&post); err != nil {
				t.Errorf("декодировать Mattermost POST: %v", err)
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			post.Id = "automation-restart-attention"
			post.CreateAt = 2_000
			post.Props = callbackServerProps(post.GetProps())
			createdMu.Lock()
			created = post.Clone()
			createdMu.Unlock()
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"id":"synthetic","message":"response lost after accept","status_code":500}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	now := start
	failingRepository := &failSecondRetainAutomationRepository{Repository: repository}
	newService := func(repo automationsrepo.Repository) *statusservice.AutomationService {
		return statusservice.NewAutomationService(statusservice.AutomationServiceConfig{
			Repository: repo, Catalog: &postgresAutomationCatalog{}, Publisher: NewControlSurface(server.URL, "synthetic-token", ""),
			StorageReady: true, Now: func() time.Time { return now },
		})
	}
	callback := statusservice.AutomationCallbackCommand{
		RunPublicID: run.PublicID, AuthenticatedProjectID: run.ProjectID,
		AuthenticatedSessionID: run.RuntimeSessionID, AuthenticatedSessionKey: run.RuntimeSessionKey,
		CallbackContractVersion: value.AutomationCallbackContractV1,
		Outcome:                 string(value.AutomationRunOutcomeRequiresHuman), AgentSummary: "Нужно решение",
		ExactPayload: []byte(`{"outcome":"requires_human","summary":"Нужно решение"}`),
	}

	first, err := newService(failingRepository).CompleteCallback(ctx, callback)
	if err != nil || first.DeliveryStatus != "pending" || first.NextAction != "retry_same_callback" {
		t.Fatalf("первая доставка=%#v error=%v", first, err)
	}
	if failingRepository.retainCalls.Load() != 2 || postCalls.Load() != 1 {
		t.Fatalf("граница отказа: retain=%d POST=%d", failingRepository.retainCalls.Load(), postCalls.Load())
	}
	persisted, err := automationspostgres.NewRepository(pool).GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || !persisted.ConfirmationPending || persisted.MattermostPostID != "" || persisted.ClaimToken == "" || persisted.Fence != 1 {
		t.Fatalf("сохранённый pre-POST fence=%#v error=%v", persisted, err)
	}
	pool.Close()
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("повторно открыть PostgreSQL pool после restart: %v", err)
	}

	now = start.Add(31 * time.Second)
	restartedRepository := automationspostgres.NewRepository(pool)
	delivered, err := newService(restartedRepository).ReconcileOwnerAttentionDeliveries(ctx, 1)
	if err == nil || delivered != 0 || postCalls.Load() != 1 {
		t.Fatalf("невидимое подтверждение после restart: delivered=%d POST=%d error=%v", delivered, postCalls.Load(), err)
	}
	stillPending, err := automationspostgres.NewRepository(pool).GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || !stillPending.ConfirmationPending || stillPending.MattermostPostID != "" || stillPending.Fence != 2 {
		t.Fatalf("сохранённое состояние после невидимого reconcile=%#v error=%v", stillPending, err)
	}

	visible.Store(true)
	now = start.Add(62 * time.Second)
	delivered, err = newService(automationspostgres.NewRepository(pool)).ReconcileOwnerAttentionDeliveries(ctx, 1)
	if err != nil || delivered != 1 {
		t.Fatalf("позднее подтверждение: delivered=%d error=%v", delivered, err)
	}
	confirmed, err := automationspostgres.NewRepository(pool).GetOwnerAttentionDelivery(ctx, run.ID)
	if err != nil || confirmed.MattermostPostID != "automation-restart-attention" || confirmed.MattermostPostCreateAt != 2_000 || confirmed.ConfirmationPending || confirmed.ClaimToken != "" || confirmed.Fence != 3 {
		t.Fatalf("подтверждённое PostgreSQL-состояние=%#v error=%v", confirmed, err)
	}
	if postCalls.Load() != 1 {
		t.Fatalf("итоговый POST count=%d, want 1", postCalls.Load())
	}
}

type failSecondRetainAutomationRepository struct {
	automationsrepo.Repository
	retainCalls atomic.Int64
}

func (repository *failSecondRetainAutomationRepository) RetainOwnerAttentionDelivery(ctx context.Context, input automationsrepo.RetainOwnerAttentionDeliveryInput) error {
	if repository.retainCalls.Add(1) == 2 {
		return errors.New("synthetic PostgreSQL failure after accepted Mattermost POST")
	}
	return repository.Repository.RetainOwnerAttentionDelivery(ctx, input)
}

type postgresAutomationCatalog struct {
	statusservice.AutomationCatalog
}

func preparePostgreSQLAutomationOwnerGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, now time.Time) (*automationspostgres.Repository, entity.ScheduledRun) {
	t.Helper()
	var projectID, roleID, chatID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Restart project', 'restart-project') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'worker', 'worker', true) returning id`, projectID).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'automation-restart-channel', 'Restart chat', 'restart-chat') returning id`, projectID).Scan(&chatID); err != nil {
		t.Fatal(err)
	}
	repository := automationspostgres.NewRepository(pool)
	schedule, created, err := repository.CreateSchedule(ctx, automationsrepo.CreateScheduleInput{
		PublicID: "schedule-88888888888888888888888888888888", ProjectID: projectID,
		TargetAgentRoleID: roleID, TargetChatID: chatID, Name: "Restart schedule",
		OwnerMattermostUserID: "root-owner-id", OwnerMattermostUserName: "root-owner",
		Preset: string(value.AutomationSchedulePresetDaily), LocalTime: "09:00", TimeZone: "UTC", NextRunAt: now.Add(time.Hour),
		PlaybookKey: value.AutomationPlaybookProjectCheckV1, PromptVersion: value.AutomationPromptVersionV1,
		PromptSnapshot: "saved", PromptSHA256: bytes.Repeat([]byte{0x41}, 32),
		CallbackContractVersion: value.AutomationCallbackContractV1,
		IdempotencyKey:          "restart-schedule", CommandHash: bytes.Repeat([]byte{0x42}, 32), Now: now,
	})
	if err != nil || !created {
		t.Fatalf("создать расписание: created=%t error=%v", created, err)
	}
	run, created, err := repository.CreateManualRun(ctx, automationsrepo.CreateManualRunInput{
		SchedulePublicID: schedule.PublicID, ProjectID: projectID, OwnerMattermostUserID: "root-owner-id",
		IdempotencyKey: "restart-run", OccurrencePublicID: "occurrence-88888888888888888888888888888888",
		RunPublicID: "scheduled-run-88888888888888888888888888888888", ScheduledFor: now,
		CallbackExpiresAt: now.Add(time.Hour), RuntimeRunID: "runtime-restart-run",
	})
	if err != nil || !created {
		t.Fatalf("создать запуск: created=%t error=%v", created, err)
	}
	var sessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id, mattermost_root_post_id,
		status, ttl_seconds, expires_at
	) values ('automation-restart-root', $1, $2, $3, 'automation-run', 'automation-restart-channel', 'automation-restart-root', 'running', 3600, $4) returning id`, projectID, chatID, roleID, now.Add(time.Hour)).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	var turnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, 'runtime-restart-run', 'automation-restart-channel', 'automation-restart-root', 'automation-restart-root', 'saved playbook', 'running') returning id`, sessionID).Scan(&turnID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set active_turn_id = $2, active_run_id = 'runtime-restart-run' where id = $1`, sessionID, turnID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RecordRunThread(ctx, automationsrepo.RecordRunThreadInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "root-owner-id",
		MattermostChannelID: "automation-restart-channel", MattermostRootPostID: "automation-restart-root", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	run, err = repository.BindRun(ctx, automationsrepo.BindRunInput{
		RunPublicID: run.PublicID, ProjectID: projectID, OwnerMattermostUserID: "root-owner-id",
		RuntimeSessionID: sessionID, RuntimeSessionKey: "automation-restart-root", RuntimeTurnID: turnID,
		RuntimeRunID: "runtime-restart-run", MattermostChannelID: "automation-restart-channel",
		MattermostRootPostID: "automation-restart-root", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminpostgres.NewRepository(pool).EnsureTurnProcess(ctx, adminrepo.EnsureTurnProcessInput{
		TurnID: turnID, ProjectID: projectID, RoleID: roleID,
		InitiatorUserID: "root-owner-id", InitiatorUserName: "root-owner", TriggerPostID: "automation-restart-root",
		MattermostChannelID: "automation-restart-channel", MattermostRootPostID: "automation-restart-root",
	}); err != nil {
		t.Fatal(err)
	}
	return repository, run
}
