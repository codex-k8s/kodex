//go:build postgres

package artifact_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	artifactpostgres "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/artifact"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryPersistsImmutableScopedArtifactAndIdempotentDelivery(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "artifact_repository")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("migrations.Run() error = %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := artifactpostgres.NewRepository(pool)
	scope := domainartifact.Scope{ProjectID: 1, ChatID: 2, SessionID: 3, RoleID: 4, TurnID: "run-1", SessionKey: "session-1", MattermostChannelID: "channel-1", MattermostRootPostID: "root-1"}
	artifactID := strings.Repeat("a", 32)
	versionID := strings.Repeat("b", 32)
	version := domainartifact.Version{
		ArtifactID: artifactID, VersionID: versionID, Scope: scope, Direction: domainartifact.DirectionInbound,
		State: domainartifact.StateUploading, StorageKey: "projects/1/sessions/3/artifacts/" + artifactID + "/versions/" + versionID,
		OriginalName: "input.txt", SafeName: "1-" + versionID + ".txt", MediaType: "text/plain", DeclaredMediaType: "text/plain",
		Size: 4, SHA256: strings.Repeat("c", 64), SourcePostID: "post-1", SourceFileID: "file-1", Ordinal: 1,
		RetentionUntil: time.Now().UTC().Add(90 * 24 * time.Hour), CreatedAt: time.Now().UTC(),
	}
	if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: version}); err != nil {
		t.Fatalf("CreateInbound() error = %v", err)
	}
	if err := repository.SetVersionState(ctx, versionID, domainartifact.StateUploading, domainartifact.StateScanning, ""); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetVersionState(ctx, versionID, domainartifact.StateScanning, domainartifact.StateAvailable, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetAvailable(ctx, scope, versionID)
	if err != nil || loaded.SHA256 != version.SHA256 || loaded.Scope.TurnID != scope.TurnID {
		t.Fatalf("GetAvailable() = %#v, error=%v", loaded, err)
	}
	foreign := scope
	foreign.SessionID = 99
	if _, err := repository.GetAvailable(ctx, foreign, versionID); !errors.Is(err, domainartifact.ErrNotFound) {
		t.Fatalf("foreign scope error = %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_artifact_versions set sha256 = repeat('d', 64) where id = $1`, versionID); !postgresCheckViolation(err) {
		t.Fatalf("immutable metadata update error = %v", err)
	}
	if err := repository.BindInbound(ctx, versionID, scope, "post-1", "file-1", 1); err != nil {
		t.Fatalf("idempotent exact binding error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
	insert into matter_codex_message_artifact_bindings(
		artifact_version_id, project_id, chat_id, session_id, turn_id,
		mattermost_post_id, mattermost_file_id, direction, ordinal
	) values ($1, 1, 2, 3, 'run-other', 'post-spoofed', 'file-1', 'inbound', 1)
	`, versionID); !postgresCheckViolation(err) {
		t.Fatalf("spoofed source binding error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_message_artifact_bindings(
	artifact_version_id, project_id, chat_id, session_id, turn_id,
	mattermost_post_id, mattermost_file_id, direction, ordinal
) values ($1, 99, 2, 3, 'run-foreign', 'post-1', 'file-1', 'inbound', 1)
`, versionID); !postgresCheckViolation(err) {
		t.Fatalf("cross-scope binding error = %v", err)
	}

	outArtifactID := strings.Repeat("d", 32)
	outVersionID := strings.Repeat("e", 32)
	deliveryID := strings.Repeat("f", 32)
	outbound := domainartifact.Version{
		ArtifactID: outArtifactID, VersionID: outVersionID, Scope: scope, Direction: domainartifact.DirectionOutbound,
		State: domainartifact.StateQuarantined, ErrorCode: "secret_detected", StorageKey: "projects/1/sessions/3/artifacts/" + outArtifactID + "/versions/" + outVersionID,
		OriginalName: "output.txt", MediaType: "text/plain", Size: 4, SHA256: strings.Repeat("1", 64),
		RetentionUntil: time.Now().UTC().Add(90 * 24 * time.Hour), CreatedAt: time.Now().UTC(),
	}
	input := domainartifact.CreateVersionInput{
		Version: outbound, IdempotencyKey: "output-v1", DeliveryID: deliveryID,
		DeliveryState: domainartifact.DeliveryQuarantined, BotTokenSecretRef: "role-bot-secret",
	}
	if err := repository.CreateOutbound(ctx, input); err != nil {
		t.Fatalf("CreateOutbound() error = %v", err)
	}
	if err := repository.CreateOutbound(ctx, input); !errors.Is(err, domainartifact.ErrConflict) {
		t.Fatalf("duplicate outbound error = %v", err)
	}
	delivery, err := repository.FindDelivery(ctx, scope, "output-v1")
	if err != nil || delivery.State != domainartifact.DeliveryQuarantined || delivery.BotTokenSecretRef != "role-bot-secret" {
		t.Fatalf("FindDelivery() = %#v, error=%v", delivery, err)
	}
}

func postgresCheckViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23514"
}
