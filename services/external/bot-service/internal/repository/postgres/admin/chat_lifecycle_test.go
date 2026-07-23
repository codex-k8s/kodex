//go:build postgres

package admin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestChatLifecycleHidesArchivedChatsFromRuntimeCatalog(t *testing.T) {
	dsn := isolatedPostgresTestDSN(t, "chat_lifecycle")
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

	var projectID, activeChatID, archivedChatID int64
	if err := pool.QueryRow(ctx, "insert into matter_codex_projects(name, slug) values ('Lifecycle', 'lifecycle') returning id").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'active-channel', 'Active', 'active') returning id", projectID).Scan(&activeChatID); err != nil {
		t.Fatalf("create active chat: %v", err)
	}
	if err := pool.QueryRow(ctx, "insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug, status, archived_at) values ($1, 'archived-channel', 'Archived', 'archived', 'archived', now()) returning id", projectID).Scan(&archivedChatID); err != nil {
		t.Fatalf("create archived chat: %v", err)
	}

	repository := postgresrepo.NewRepository(pool)
	chats, err := repository.ListChats(ctx, projectID)
	if err != nil {
		t.Fatalf("list chats: %v", err)
	}
	if len(chats) != 1 || chats[0].ID != activeChatID || chats[0].Status != "active" || chats[0].ArchivedAt != nil {
		t.Fatalf("active chats = %#v", chats)
	}
	if _, err := repository.GetChatByMattermostChannelID(ctx, "archived-channel"); !errors.Is(err, domainrepo.ErrNotFound) {
		t.Fatalf("archived channel runtime lookup error = %v", err)
	}
	archived, err := repository.GetChat(ctx, archivedChatID)
	if err != nil {
		t.Fatalf("get archived chat by id: %v", err)
	}
	if archived.Status != "archived" || archived.ArchivedAt == nil {
		t.Fatalf("archived chat = %#v", archived)
	}
}
