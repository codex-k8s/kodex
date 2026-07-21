//go:build postgres

package migrations_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
	postgresrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/admin"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/migrations"
)

func TestRuntimeRevisionMigrationUpgradesV33WithLegacySessionAndQueue(t *testing.T) {
	dsn := isolatedMigrationDSN(t, "runtime_revision_v33")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := migrations.RunTo(ctx, dsn, 33); err != nil {
		t.Fatalf("prepare v33 schema: %v", err)
	}
	pool := openMigrationPool(t, ctx, dsn)
	repository := postgresrepo.NewRepository(pool)
	project, _, err := repository.UpsertProject(ctx, adminrepo.UpsertProjectInput{
		Name: "Runtime upgrade", Slug: "runtime-upgrade", AdvancedSettings: "{}",
	})
	if err != nil {
		pool.Close()
		t.Fatalf("seed project: %v", err)
	}
	role, _, err := repository.UpsertAgentRole(ctx, adminrepo.UpsertAgentRoleInput{
		ProjectID: project.ID, Name: "developer", RoleType: "worker", PromptMode: "template",
		OpenAIAccountName: "primary", KubernetesAccess: "read-only", SandboxMode: "danger-full-access",
		AdvancedSettings: "{}", Enabled: true,
	})
	if err != nil {
		pool.Close()
		t.Fatalf("seed role: %v", err)
	}
	chat, _, err := repository.CreateChat(ctx, adminrepo.CreateChatInput{
		ProjectID: project.ID, MattermostChannelID: "channel-runtime-upgrade", Name: "Runtime upgrade chat",
		Slug: "runtime-upgrade-chat", ChatType: "custom", Settings: "{}", RoleIDs: []int64{role.ID},
	})
	if err != nil {
		pool.Close()
		t.Fatalf("seed chat: %v", err)
	}
	session, _, err := repository.UpsertAgentSession(ctx, adminrepo.UpsertAgentSessionInput{
		SessionKey: "runtime-upgrade-session", ProjectID: project.ID, ChatID: chat.ID, RoleID: role.ID,
		SessionScope: "thread", MattermostChannelID: "channel-runtime-upgrade", OpenAIAccountName: "primary",
		TTLSeconds: 3600, Capabilities: "{}",
	})
	if err != nil {
		pool.Close()
		t.Fatalf("seed v33 session: %v", err)
	}
	legacyPayload, legacyRaw := migrationArchiveFixture(t, "legacy-bounded-archive")
	if _, err := repository.UpdateAgentSessionSnapshot(ctx, adminrepo.UpdateAgentSessionSnapshotInput{
		SessionKey: session.SessionKey, CodexSessionID: "legacy-codex-safe-id",
		SessionArchiveGzipBase64: legacyPayload, Status: "idle", ExtendTTLSeconds: 3600,
	}); err != nil {
		pool.Close()
		t.Fatalf("seed legacy archive: %v", err)
	}
	if _, err := pool.Exec(ctx, `
insert into matter_codex_agent_session_turns(
	session_id, run_id, mattermost_channel_id, mattermost_root_post_id,
	mattermost_post_id, user_id, user_name, message
) values ($1, 'legacy-queued-run', 'channel-runtime-upgrade', '', 'legacy-post', 'legacy-user', 'developer', 'legacy queued turn')
`, session.ID); err != nil {
		pool.Close()
		t.Fatalf("seed v33 queued turn: %v", err)
	}
	pool.Close()

	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("upgrade v33->v34: %v", err)
	}
	if version, err := migrations.Version(ctx, dsn); err != nil || version != 34 {
		t.Fatalf("schema version after runtime upgrade = %d, error=%v", version, err)
	}
	pool = openMigrationPool(t, ctx, dsn)
	defer pool.Close()
	repository = postgresrepo.NewRepository(pool)
	archive, err := repository.GetLatestAgentSessionArchive(ctx, session.ID)
	if err != nil {
		t.Fatalf("read migrated legacy archive: %v", err)
	}
	digest := sha256.Sum256(legacyRaw)
	if archive.Version != 0 || archive.PayloadGzipBase64 != legacyPayload || archive.SHA256 != hex.EncodeToString(digest[:]) || archive.SizeBytes != int64(len(legacyRaw)) {
		t.Fatalf("legacy archive compatibility = %#v", archive)
	}
	var archiveVersion int64
	var queuedCount int
	if err := pool.QueryRow(ctx, `select archive_version from matter_codex_agent_sessions where id = $1`, session.ID).Scan(&archiveVersion); err != nil {
		t.Fatalf("read additive archive metadata: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from matter_codex_agent_session_turns where session_id = $1 and status = 'queued'`, session.ID).Scan(&queuedCount); err != nil {
		t.Fatalf("read preserved queue: %v", err)
	}
	if archiveVersion != 0 || queuedCount != 1 {
		t.Fatalf("additive upgrade state: archive_version=%d queued=%d", archiveVersion, queuedCount)
	}
	if _, err := pool.Exec(ctx, `update matter_codex_agent_sessions set openai_account_name = 'secondary' where id = $1`, session.ID); err == nil {
		t.Fatal("upgraded session account affinity accepted mutation")
	}
	if err := migrations.Run(ctx, dsn); err != nil {
		t.Fatalf("repeated v34 up: %v", err)
	}
}

func migrationArchiveFixture(t *testing.T, value string) (string, []byte) {
	t.Helper()
	var raw bytes.Buffer
	gzipWriter := gzip.NewWriter(&raw)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions", Typeflag: tar.TypeDir, Mode: 0o700, Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	body := []byte(value)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "sessions/state.json", Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(body)), Format: tar.FormatUSTAR}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw.Bytes()), raw.Bytes()
}
