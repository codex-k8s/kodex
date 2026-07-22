//go:build postgres

package artifact_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/artifacttype"
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
	fixture := createArtifactPostgresFixture(t, ctx, pool)
	scope := createArtifactTurnScope(t, ctx, pool, fixture, "run-1", "running")
	artifactID := strings.Repeat("a", 32)
	versionID := strings.Repeat("b", 32)
	version := domainartifact.Version{
		ArtifactID: artifactID, VersionID: versionID, Scope: scope, Direction: domainartifact.DirectionInbound,
		State:        domainartifact.StateUploading,
		StorageKey:   fmt.Sprintf("projects/%d/sessions/%d/artifacts/%s/versions/%s", scope.ProjectID, scope.SessionID, artifactID, versionID),
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
		artifact_version_id, project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id,
		mattermost_post_id, mattermost_file_id, direction, ordinal
	) values ($1, $2, $3, $4, $5, $6, $7, 'post-spoofed', 'file-1', 'inbound', 1)
	`, versionID, scope.ProjectID, scope.ChatID, scope.SessionID, scope.RoleID, scope.RuntimeTurnID, scope.TurnID); !postgresScopeViolation(err) {
		t.Fatalf("spoofed source binding error = %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_message_artifact_bindings(
	artifact_version_id, project_id, chat_id, session_id, role_id, runtime_turn_id, turn_id,
	mattermost_post_id, mattermost_file_id, direction, ordinal
) values ($1, 999999, $2, $3, $4, $5, $6, 'post-1', 'file-1', 'inbound', 1)
`, versionID, scope.ChatID, scope.SessionID, scope.RoleID, scope.RuntimeTurnID, scope.TurnID); !postgresScopeViolation(err) {
		t.Fatalf("cross-scope binding error = %v", err)
	}

	outArtifactID := strings.Repeat("d", 32)
	outVersionID := strings.Repeat("e", 32)
	deliveryID := strings.Repeat("f", 32)
	outbound := domainartifact.Version{
		ArtifactID: outArtifactID, VersionID: outVersionID, Scope: scope, Direction: domainartifact.DirectionOutbound,
		State: domainartifact.StateQuarantined, ErrorCode: "secret_detected",
		StorageKey:   fmt.Sprintf("projects/%d/sessions/%d/artifacts/%s/versions/%s", scope.ProjectID, scope.SessionID, outArtifactID, outVersionID),
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

func TestRepositoryRoundTripsEverySupportedMediaTypeAndRejectsUnsafeScopeBeforeInsert(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "artifact_media_contract")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	fixture := createArtifactPostgresFixture(t, ctx, pool)

	for index, format := range artifacttype.SupportedFormats() {
		t.Run(format.MediaType, func(t *testing.T) {
			runID := fmt.Sprintf("media-contract-%d", index+1)
			scope := createArtifactTurnScope(t, ctx, pool, fixture, runID, "running")
			artifactID := fmt.Sprintf("%032x", index*4+1)
			versionID := fmt.Sprintf("%032x", index*4+2)
			version := artifactContractVersion(scope, artifactID, versionID, format, domainartifact.DirectionInbound, index+1)
			if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: version}); err != nil {
				t.Fatalf("CreateInbound(%s) error = %v", format.MediaType, err)
			}
			if err := repository.SetVersionState(ctx, versionID, domainartifact.StateUploading, domainartifact.StateScanning, ""); err != nil {
				t.Fatal(err)
			}
			if err := repository.SetVersionState(ctx, versionID, domainartifact.StateScanning, domainartifact.StateAvailable, ""); err != nil {
				t.Fatal(err)
			}
			loaded, err := repository.GetAvailable(ctx, scope, versionID)
			if err != nil || loaded.MediaType != format.MediaType || loaded.Scope.RuntimeTurnID != scope.RuntimeTurnID {
				t.Fatalf("GetAvailable(%s) = %#v, error=%v", format.MediaType, loaded, err)
			}
			listed, err := repository.ListTurn(ctx, scope)
			if err != nil || len(listed) != 1 || listed[0].MediaType != format.MediaType {
				t.Fatalf("ListTurn(%s) = %#v, error=%v", format.MediaType, listed, err)
			}

			outArtifactID := fmt.Sprintf("%032x", index*4+3)
			outVersionID := fmt.Sprintf("%032x", index*4+4)
			deliveryID := fmt.Sprintf("%032x", index+10_000)
			outbound := artifactContractVersion(scope, outArtifactID, outVersionID, format, domainartifact.DirectionOutbound, index+1)
			if err := repository.CreateOutbound(ctx, domainartifact.CreateVersionInput{
				Version: outbound, IdempotencyKey: "delivery-" + runID, DeliveryID: deliveryID,
				DeliveryState: domainartifact.DeliveryPending, BotTokenSecretRef: "synthetic-role-bot-secret",
			}); err != nil {
				t.Fatalf("CreateOutbound(%s) error = %v", format.MediaType, err)
			}
			if err := repository.SetVersionState(ctx, outVersionID, domainartifact.StateUploading, domainartifact.StateScanning, ""); err != nil {
				t.Fatal(err)
			}
			if err := repository.SetVersionState(ctx, outVersionID, domainartifact.StateScanning, domainartifact.StateAvailable, ""); err != nil {
				t.Fatal(err)
			}
			delivery, err := repository.FindDelivery(ctx, scope, "delivery-"+runID)
			if err != nil || delivery.ArtifactVersion.MediaType != format.MediaType || delivery.Scope.RuntimeTurnID != scope.RuntimeTurnID {
				t.Fatalf("FindDelivery(%s) = %#v, error=%v", format.MediaType, delivery, err)
			}
		})
	}

	scope := createArtifactTurnScope(t, ctx, pool, fixture, "media-denied", "running")
	denied := artifactContractVersion(scope, strings.Repeat("d", 32), strings.Repeat("e", 32), artifacttype.Format{MediaType: "application/vnd.ms-word.document.macroenabled.12", Extension: ".docm"}, domainartifact.DirectionInbound, 1)
	if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: denied}); !errors.Is(err, domainartifact.ErrMediaTypeDenied) {
		t.Fatalf("macro-enabled MIME error = %v", err)
	}
	nonexistent := artifactContractVersion(scope, strings.Repeat("1", 32), strings.Repeat("2", 32), artifacttype.Format{MediaType: "text/plain", Extension: ".txt"}, domainartifact.DirectionInbound, 2)
	nonexistent.Scope.RuntimeTurnID += 1_000_000
	if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: nonexistent}); !errors.Is(err, domainartifact.ErrScopeDenied) {
		t.Fatalf("nonexistent turn error = %v", err)
	}
	var otherProjectID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Other artifact scope', 'other-artifact-scope') returning id`).Scan(&otherProjectID); err != nil {
		t.Fatal(err)
	}
	wrongScope := artifactContractVersion(scope, strings.Repeat("3", 32), strings.Repeat("4", 32), artifacttype.Format{MediaType: "text/plain", Extension: ".txt"}, domainartifact.DirectionInbound, 3)
	wrongScope.Scope.ProjectID = otherProjectID
	wrongScope.StorageKey = fmt.Sprintf("projects/%d/sessions/%d/artifacts/%s/versions/%s", otherProjectID, scope.SessionID, wrongScope.ArtifactID, wrongScope.VersionID)
	if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: wrongScope}); !errors.Is(err, domainartifact.ErrScopeDenied) {
		t.Fatalf("wrong-scope turn error = %v", err)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_session_turns set status = 'canceled' where id = $1`, scope.RuntimeTurnID); err != nil {
		t.Fatal(err)
	}
	revoked := artifactContractVersion(scope, strings.Repeat("5", 32), strings.Repeat("6", 32), artifacttype.Format{MediaType: "text/plain", Extension: ".txt"}, domainartifact.DirectionInbound, 4)
	if err := repository.CreateInbound(ctx, domainartifact.CreateVersionInput{Version: revoked}); !errors.Is(err, domainartifact.ErrScopeDenied) {
		t.Fatalf("revoked turn error = %v", err)
	}
}

func TestPostgresArtifactServiceRecoversObjectFailureWithOneTurnObjectAndBinding(t *testing.T) {
	dsn := testsupport.IsolatedSchemaDSN(t, "artifact_object_recovery")
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
	fixture := createArtifactPostgresFixture(t, ctx, pool)
	scope := createArtifactTurnScope(t, ctx, pool, fixture, "object-recovery", "admitting")
	body := []byte("name,count\nalpha,1\nbeta,2\n")
	objectStore := &recoveringPostgresObjectStore{objects: map[string][]byte{}, failOnce: true}
	service, err := domainartifact.NewService(domainartifact.ServiceConfig{
		Repository: artifactpostgres.NewRepository(pool),
		Source:     recoveringPostgresSource{body: body}, ObjectStore: objectStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := domainartifact.IngestInput{
		Scope: scope, SourcePostID: "post-1", SourceUserID: "user-1", FileIDs: []string{"file-1"},
	}
	if _, err := service.IngestIncoming(ctx, input); err == nil {
		t.Fatal("первая object-store запись должна вернуть ошибку")
	}
	manifest, err := service.IngestIncoming(ctx, input)
	if err != nil || len(manifest.Files) != 1 || manifest.Files[0].MediaType != "text/csv" || !strings.HasSuffix(manifest.Files[0].LocalPath, ".csv") {
		t.Fatalf("recovery manifest=%#v error=%v", manifest, err)
	}
	var artifactCount, versionCount, bindingCount, turnCount int
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_artifacts`).Scan(&artifactCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_artifact_versions`).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_message_artifact_bindings`).Scan(&bindingCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_session_turns`).Scan(&turnCount); err != nil {
		t.Fatal(err)
	}
	if artifactCount != 1 || versionCount != 1 || bindingCount != 1 || turnCount != 1 || len(objectStore.objects) != 1 || objectStore.puts != 1 {
		t.Fatalf("retry создал дубликат: artifacts=%d versions=%d bindings=%d turns=%d objects=%d puts=%d", artifactCount, versionCount, bindingCount, turnCount, len(objectStore.objects), objectStore.puts)
	}
}

func postgresCheckViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23514"
}

func postgresScopeViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "23503" || postgresError.Code == "23514")
}

type artifactPostgresFixture struct {
	projectID int64
	chatID    int64
	roleID    int64
	sessionID int64
}

func createArtifactPostgresFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) artifactPostgresFixture {
	t.Helper()
	var fixture artifactPostgresFixture
	if err := pool.QueryRow(ctx, `insert into matter_codex_projects(name, slug) values ('Artifact contract', 'artifact-contract') returning id`).Scan(&fixture.projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_roles(project_id, name, role_type, enabled) values ($1, 'artifact-worker', 'worker', true) returning id`, fixture.projectID).Scan(&fixture.roleID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_chats(project_id, mattermost_channel_id, name, slug) values ($1, 'artifact-channel', 'Artifact chat', 'artifact-chat') returning id`, fixture.projectID).Scan(&fixture.chatID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_sessions(
		session_key, project_id, chat_id, role_id, session_scope, mattermost_channel_id,
		mattermost_root_post_id, status, ttl_seconds, expires_at
	) values ('artifact-session', $1, $2, $3, 'artifact-test', 'artifact-channel', 'artifact-root', 'running', 3600, now() + interval '1 hour') returning id`, fixture.projectID, fixture.chatID, fixture.roleID).Scan(&fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func createArtifactTurnScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture artifactPostgresFixture, runID string, status string) domainartifact.Scope {
	t.Helper()
	var turnID int64
	if err := pool.QueryRow(ctx, `insert into matter_codex_agent_session_turns(
		session_id, run_id, mattermost_channel_id, mattermost_root_post_id, mattermost_post_id, message, status
	) values ($1, $2, 'artifact-channel', 'artifact-root', $2, 'artifact contract', $3) returning id`, fixture.sessionID, runID, status).Scan(&turnID); err != nil {
		t.Fatal(err)
	}
	return domainartifact.Scope{
		ProjectID: fixture.projectID, ChatID: fixture.chatID, SessionID: fixture.sessionID, RoleID: fixture.roleID,
		RuntimeTurnID: turnID, TurnID: runID, SessionKey: "artifact-session",
		MattermostChannelID: "artifact-channel", MattermostRootPostID: "artifact-root",
	}
}

func artifactContractVersion(scope domainartifact.Scope, artifactID string, versionID string, format artifacttype.Format, direction domainartifact.Direction, ordinal int) domainartifact.Version {
	version := domainartifact.Version{
		ArtifactID: artifactID, VersionID: versionID, Scope: scope, Direction: direction,
		State:        domainartifact.StateUploading,
		StorageKey:   fmt.Sprintf("projects/%d/sessions/%d/artifacts/%s/versions/%s", scope.ProjectID, scope.SessionID, artifactID, versionID),
		OriginalName: "contract" + format.Extension, SafeName: fmt.Sprintf("%d-%s%s", max(ordinal, 1), versionID, format.Extension),
		MediaType: format.MediaType, DeclaredMediaType: format.MediaType, Size: 1, SHA256: fmt.Sprintf("%064x", ordinal+1),
		SourcePostID: scope.TurnID, SourceFileID: "file-" + scope.TurnID, Ordinal: 1,
		RetentionUntil: time.Now().UTC().Add(90 * 24 * time.Hour), CreatedAt: time.Now().UTC(),
	}
	if direction == domainartifact.DirectionOutbound {
		version.SourcePostID = ""
		version.SourceFileID = ""
	}
	return version
}

type recoveringPostgresSource struct {
	body []byte
}

func (source recoveringPostgresSource) Metadata(_ context.Context, fileID string) (domainartifact.SourceFile, error) {
	if fileID != "file-1" {
		return domainartifact.SourceFile{}, domainartifact.ErrNotFound
	}
	return domainartifact.SourceFile{
		FileID: fileID, PostID: "post-1", ChannelID: "artifact-channel", CreatorID: "user-1",
		OriginalName: "untrusted.bin", DeclaredMediaType: "application/octet-stream", DeclaredSize: int64(len(source.body)),
	}, nil
}

func (source recoveringPostgresSource) Open(_ context.Context, fileID string) (io.ReadCloser, error) {
	if fileID != "file-1" {
		return nil, domainartifact.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(source.body)), nil
}

type recoveringPostgresObjectStore struct {
	objects  map[string][]byte
	puts     int
	failOnce bool
}

func (store *recoveringPostgresObjectStore) PutImmutable(_ context.Context, key string, _ string, size int64, _ string, body io.Reader) error {
	if store.failOnce {
		store.failOnce = false
		return errors.New("synthetic object-store failure")
	}
	if _, exists := store.objects[key]; exists {
		return domainartifact.ErrConflict
	}
	value, err := io.ReadAll(body)
	if err != nil || int64(len(value)) != size {
		return domainartifact.ErrConflict
	}
	store.objects[key] = value
	store.puts++
	return nil
}

func (store *recoveringPostgresObjectStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	value, exists := store.objects[key]
	if !exists {
		return nil, domainartifact.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}
