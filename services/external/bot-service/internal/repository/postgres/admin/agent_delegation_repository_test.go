package admin_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentDelegationRepositoryLifecycle(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "delegation")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	var projectID, sourceRoleID, targetRoleID, sourceChatID, targetChatID int64
	if err := pool.QueryRow(ctx, "insert into matter_codex_projects(name, slug) values ('Test', 'test') returning id").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'source', 'manager') returning id", projectID).Scan(&sourceRoleID); err != nil {
		t.Fatalf("create source role: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_agent_roles(project_id, name, role_type) values ($1, 'target', 'worker') returning id", projectID).Scan(&targetRoleID); err != nil {
		t.Fatalf("create target role: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, name, slug) values ($1, 'Source', 'source') returning id", projectID).Scan(&sourceChatID); err != nil {
		t.Fatalf("create source chat: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, name, slug) values ($1, 'Target', 'target') returning id", projectID).Scan(&targetChatID); err != nil {
		t.Fatalf("create target chat: %v", err)
	}

	expiresAt := time.Now().UTC().Add(time.Hour)
	var sourceSessionID, targetSessionID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, ttl_seconds, expires_at
	) values ('source-session', $1, $2, $3, 'thread_role', 3600, $4) returning id`, projectID, sourceChatID, sourceRoleID, expiresAt).Scan(&sourceSessionID); err != nil {
		t.Fatalf("create source session: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, ttl_seconds, expires_at
	) values ('target-session', $1, $2, $3, 'thread_role', 3600, $4) returning id`, projectID, targetChatID, targetRoleID, expiresAt).Scan(&targetSessionID); err != nil {
		t.Fatalf("create target session: %v", err)
	}
	var sourceTurnID, targetTurnID, callbackTurnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'source-run', 'source-channel', 'source-root', 'source-post', 'source') returning id`, sourceSessionID).Scan(&sourceTurnID); err != nil {
		t.Fatalf("create source turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'target-run', 'target-channel', 'target-root', 'target-post', 'target') returning id`, targetSessionID).Scan(&targetTurnID); err != nil {
		t.Fatalf("create target turn: %v", err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message
	) values ($1, 'callback-run', 'source-channel', 'source-root', 'callback-post', 'callback') returning id`, sourceSessionID).Scan(&callbackTurnID); err != nil {
		t.Fatalf("create callback turn: %v", err)
	}

	repository := postgresrepo.NewRepository(pool)
	created, wasCreated, err := repository.CreateAgentDelegation(ctx, domainrepo.CreateAgentDelegationInput{
		ProjectID:       projectID,
		SourceSessionID: sourceSessionID,
		SourceTurnID:    sourceTurnID,
		TargetChatID:    targetChatID,
		TargetRoleID:    targetRoleID,
		WorkItemKey:     "work-1",
		Title:           "Work 1",
	})
	if err != nil || !wasCreated {
		t.Fatalf("CreateAgentDelegation() item=%#v created=%t error=%v", created, wasCreated, err)
	}
	duplicate, wasCreated, err := repository.CreateAgentDelegation(ctx, domainrepo.CreateAgentDelegationInput{
		ProjectID:       projectID,
		SourceSessionID: sourceSessionID,
		SourceTurnID:    sourceTurnID,
		TargetChatID:    targetChatID,
		TargetRoleID:    targetRoleID,
		WorkItemKey:     "work-1",
		Title:           "Work 1",
	})
	if err != nil || wasCreated || duplicate.ID != created.ID {
		t.Fatalf("duplicate item=%#v created=%t error=%v", duplicate, wasCreated, err)
	}
	if _, err := repository.SetAgentDelegationRoot(ctx, created.ID, "target-root"); err != nil {
		t.Fatalf("SetAgentDelegationRoot() error = %v", err)
	}
	started, err := repository.SetAgentDelegationTarget(ctx, created.ID, targetSessionID, targetTurnID, "target-run")
	if err != nil || started.Status != "queued" {
		t.Fatalf("started=%#v error=%v", started, err)
	}
	items, err := repository.ListAgentDelegationsBySource(ctx, sourceSessionID, 20)
	if err != nil || len(items) != 1 || items[0].TargetRootPostID != "target-root" {
		t.Fatalf("items=%#v error=%v", items, err)
	}
	callbackTarget, err := repository.GetAgentDelegationForCallback(ctx, targetSessionID)
	if err != nil || callbackTarget.ID != created.ID {
		t.Fatalf("callback target=%#v error=%v", callbackTarget, err)
	}
	callback, err := repository.SetAgentDelegationCallback(ctx, created.ID, callbackTurnID, "callback-run")
	if err != nil || callback.CallbackRunID != "callback-run" || callback.Status != "callback_queued" {
		t.Fatalf("callback=%#v error=%v", callback, err)
	}
	callbackProps := func(destination string, publication string, event string, externalID string, payloadHash []byte) []byte {
		encoded, encodeErr := json.Marshal(map[string]string{
			"matter_codex_event":                   event,
			"matter_codex_callback_delivery_id":    externalID,
			"matter_codex_callback_delegation_id":  strconv.FormatInt(created.ID, 10),
			"matter_codex_callback_run_id":         "callback-run",
			"matter_codex_callback_destination":    destination,
			"matter_codex_callback_publication":    publication,
			"matter_codex_callback_payload_sha256": hex.EncodeToString(payloadHash),
		})
		if encodeErr != nil {
			t.Fatalf("encode callback props: %v", encodeErr)
		}
		return encoded
	}
	sourceHash := bytes.Repeat([]byte{0xab}, 32)
	childHash := bytes.Repeat([]byte{0xcd}, 32)
	deliveryInputs := []domainrepo.CreateAgentDelegationCallbackDeliveryInput{
		{
			DelegationID: created.ID, CallbackRunID: "callback-run", Destination: "source_callback", Publication: "agent_cross_chat_callback:0001",
			ChannelID: "source-channel", RootPostID: "source-root", Message: "source audit",
			PropsJSON:     callbackProps("source_callback", "agent_cross_chat_callback:0001", "agent_cross_chat_callback", "aaaaaaaaaaaaaaaaaaaaaaaaaa", sourceHash),
			PayloadSHA256: sourceHash, ExternalID: "aaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			DelegationID: created.ID, CallbackRunID: "callback-run", Destination: "child_return", Publication: "agent_cross_chat_callback_returned:0001",
			ChannelID: "target-channel", RootPostID: "target-root", Message: "child audit",
			PropsJSON:     callbackProps("child_return", "agent_cross_chat_callback_returned:0001", "agent_cross_chat_callback_returned", "bbbbbbbbbbbbbbbbbbbbbbbbbb", childHash),
			PayloadSHA256: childHash, ExternalID: "bbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}
	deliveries, err := repository.CreateAgentDelegationCallbackDeliveries(ctx, deliveryInputs)
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("CreateAgentDelegationCallbackDeliveries() items=%#v error=%v", deliveries, err)
	}
	repeated, err := repository.CreateAgentDelegationCallbackDeliveries(ctx, deliveryInputs)
	if err != nil || len(repeated) != 2 || repeated[0].ID != deliveries[0].ID || repeated[1].ID != deliveries[1].ID {
		t.Fatalf("idempotent outbox insert items=%#v error=%v", repeated, err)
	}
	conflicting := deliveryInputs[0]
	conflicting.Message = "foreign payload"
	if _, err := repository.CreateAgentDelegationCallbackDeliveries(ctx, []domainrepo.CreateAgentDelegationCallbackDeliveryInput{conflicting}); err == nil {
		t.Fatal("immutable callback delivery plan accepted a conflicting payload")
	}
	now := time.Now().UTC()
	claimed, err := repository.ClaimAgentDelegationCallbackDelivery(ctx, domainrepo.ClaimAgentDelegationCallbackDeliveryInput{
		DelegationID: created.ID, CallbackRunID: "callback-run", Now: now,
		LeaseOwner: "lease-source", LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || claimed.Status != "in_flight" || claimed.AttemptCount != 1 {
		t.Fatalf("ClaimAgentDelegationCallbackDelivery() item=%#v error=%v", claimed, err)
	}
	if _, err := repository.DeliverAgentDelegationCallbackDelivery(ctx, domainrepo.DeliverAgentDelegationCallbackDeliveryInput{
		ID: claimed.ID, LeaseOwner: "foreign-lease", MattermostPostID: "post-source", Now: now,
	}); !errors.Is(err, domainrepo.ErrNotFound) {
		t.Fatalf("foreign lease delivery error=%v", err)
	}
	delivered, err := repository.DeliverAgentDelegationCallbackDelivery(ctx, domainrepo.DeliverAgentDelegationCallbackDeliveryInput{
		ID: claimed.ID, LeaseOwner: claimed.LeaseOwner, MattermostPostID: "post-source", Now: now,
	})
	if err != nil || delivered.Status != "delivered" || delivered.MattermostPostID != "post-source" {
		t.Fatalf("DeliverAgentDelegationCallbackDelivery() item=%#v error=%v", delivered, err)
	}
	remaining, err := repository.ClaimAgentDelegationCallbackDelivery(ctx, domainrepo.ClaimAgentDelegationCallbackDeliveryInput{
		DelegationID: created.ID, CallbackRunID: "callback-run", Now: now,
		LeaseOwner: "lease-child", LeaseUntil: now.Add(time.Minute),
	})
	if err != nil || remaining.ID == claimed.ID || remaining.Destination != "child_return" {
		t.Fatalf("partial outcome claim item=%#v error=%v", remaining, err)
	}
	if _, err := repository.ReleaseAgentDelegationCallbackDelivery(ctx, domainrepo.ReleaseAgentDelegationCallbackDeliveryInput{
		ID: remaining.ID, LeaseOwner: remaining.LeaseOwner, Status: "pending", LastErrorCode: "mattermost_unconfirmed", Now: now,
	}); err != nil {
		t.Fatalf("ReleaseAgentDelegationCallbackDelivery() error=%v", err)
	}
	listed, err := repository.ListAgentDelegationCallbackDeliveries(ctx, created.ID, "callback-run")
	if err != nil || len(listed) != 2 || listed[0].Status == listed[1].Status {
		t.Fatalf("partial durable state items=%#v error=%v", listed, err)
	}
	if _, err := pool.Exec(ctx, "update matter_codex_agent_session_turns set status = 'succeeded' where id = $1", callbackTurnID); err != nil {
		t.Fatalf("complete callback turn: %v", err)
	}
	items, err = repository.ListAgentDelegationsBySource(ctx, sourceSessionID, 20)
	if err != nil || len(items) != 1 || items[0].Status != "callback_succeeded" {
		t.Fatalf("completed callback items=%#v error=%v", items, err)
	}
	separateTurn, wasCreated, err := repository.CreateAgentDelegation(ctx, domainrepo.CreateAgentDelegationInput{
		ProjectID:       projectID,
		SourceSessionID: sourceSessionID,
		SourceTurnID:    callbackTurnID,
		TargetChatID:    targetChatID,
		TargetRoleID:    targetRoleID,
		WorkItemKey:     "work-1",
		Title:           "Work 1 retry in a new process turn",
	})
	if err != nil || !wasCreated || separateTurn.ID == created.ID {
		t.Fatalf("separate source turn item=%#v created=%t error=%v", separateTurn, wasCreated, err)
	}
}
