package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

type avatarLifecycleObjectStore struct {
	*objectstoragetest.Store
	afterPut func(objectstorage.Receipt)
}

func (store *avatarLifecycleObjectStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	receipt, err := store.Store.Put(ctx, input)
	if err == nil && store.afterPut != nil {
		callback := store.afterPut
		store.afterPut = nil
		callback(receipt)
	}
	return receipt, err
}

func TestAvatarLifecycleComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	objects := &avatarLifecycleObjectStore{Store: objectstoragetest.New()}
	repository, err := New(pool, "openai-codex", "gpt-5", objects)
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatalf("configure provider credential: %v", err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatalf("configure role images: %v", err)
	}
	var bootstrapped bool
	if err := pool.QueryRow(ctx, `SELECT bootstrapped_at IS NOT NULL FROM control_plane.installation WHERE singleton`).Scan(&bootstrapped); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if !bootstrapped {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap repository: %v", err)
		}
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID:     "20000000-0000-4000-8000-000000000001",
		ExternalTenantID:    "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Avatar lifecycle owner",
		CallerWorkload:      "control-api-gateway",
		Operation:           "platform.command.projects.create",
	}, "control-api-gateway")

	primary := createAvatarProject(t, ctx, service, owner, "avatar-primary")
	secondary := createAvatarProject(t, ctx, service, owner, "avatar-secondary")
	agent := createLifecycleAgent(t, ctx, service, owner, primary.Ref, "avatar-agent-create", "Avatar owner")
	agent = testAtomicAvatarUpload(t, ctx, pool, repository, service, objects, owner, agent, primary.Ref)
	testExpiredAvatarReservationCleanup(t, ctx, pool, repository, objects, owner, agent, primary.Ref)

	assertAvatarUpdateError(t, ctx, service, owner, agent, "https://example.invalid/avatar.png", "avatar-reject-external", domainerrs.ErrInvalid)
	textArtifact := uploadAvatarFixture(t, ctx, service, owner, primary.Ref, "avatar-note.txt", "text/plain", []byte("not an image"), "avatar-text")
	assertAvatarUpdateError(t, ctx, service, owner, agent, avatarArtifactContentURL(textArtifact.Ref), "avatar-reject-text", domainerrs.ErrInvalid)
	foreignArtifact := uploadAvatarFixture(t, ctx, service, owner, secondary.Ref, "agent-avatar.png", "image/png", avatarPNG(t), "avatar-foreign")
	assertAvatarUpdateError(t, ctx, service, owner, agent, avatarArtifactContentURL(foreignArtifact.Ref), "avatar-reject-foreign", domainerrs.ErrInvalid)

	first := uploadAvatarFixture(t, ctx, service, owner, primary.Ref, "agent-avatar.png", "image/png", avatarPNG(t), "avatar-first")
	updated := setAvatar(t, ctx, service, owner, agent, first, "avatar-apply-first")
	if updated.AvatarURL != avatarArtifactContentURL(first.Ref) || updated.Avatar.ArtifactRef != first.Ref ||
		updated.Avatar.ArtifactRevision != first.Revision || updated.Avatar.Source != "ARTIFACT" {
		t.Fatalf("agent lost immutable avatar revision: %#v", updated)
	}
	second := uploadAvatarFixture(t, ctx, service, owner, primary.Ref, "agent-avatar.png", "image/png", avatarPNG(t), "avatar-second")
	if second.Ref == first.Ref || second.Revision != first.Revision+1 {
		t.Fatalf("avatar crop did not create an immutable revision: first=%#v second=%#v", first, second)
	}
	updated = setAvatar(t, ctx, service, owner, updated, second, "avatar-apply-second")
	assertRetiredAvatar(t, ctx, pool, first.Ref, agent.Ref)

	updated = removeAvatar(t, ctx, service, owner, updated, "avatar-clear")
	if updated.AvatarURL != "" || updated.Avatar.Source != "FALLBACK" || updated.Avatar.ArtifactRef != "" {
		t.Fatalf("avatar removal did not restore generated fallback: %#v", updated.Avatar)
	}
	assertRetiredAvatar(t, ctx, pool, second.Ref, agent.Ref)
	assertAvatarUpdateError(t, ctx, service, owner, updated, avatarArtifactContentURL(second.Ref), "avatar-reject-deleted", domainerrs.ErrInvalid)
}

func testAtomicAvatarUpload(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *Repository,
	service *platformservice.Service,
	objects *avatarLifecycleObjectStore,
	owner value.Principal,
	agent entity.Agent,
	projectRef string,
) entity.Agent {
	t.Helper()
	body := avatarPNG(t)
	upload := func(key string, version int64) (entity.Agent, error) {
		return service.UploadAgentAvatar(ctx, owner, value.Mutation{IdempotencyKey: key}, platformrepo.AgentAvatarUpload{
			ArtifactUpload: platformrepo.ArtifactUpload{
				ProjectRef: projectRef, FileName: "atomic-avatar.png", MediaType: "image/png",
				SizeBytes: int64(len(body)), Reader: bytes.NewReader(body),
			},
			AgentRef: agent.Ref, ExpectedVersion: version,
		})
	}
	updated, err := upload("avatar-atomic-finalize", agent.Version)
	if err != nil || updated.Avatar.ArtifactRef == "" || updated.Avatar.Source != "ARTIFACT" || updated.Version != agent.Version+1 {
		t.Fatalf("atomic avatar finalize: agent=%#v err=%v", updated, err)
	}
	replayed, err := upload("avatar-atomic-finalize", agent.Version)
	if err != nil || replayed.Avatar.ArtifactRef != updated.Avatar.ArtifactRef || replayed.Version != updated.Version {
		t.Fatalf("atomic avatar replay: agent=%#v err=%v", replayed, err)
	}
	var finalizedState string
	if err := pool.QueryRow(ctx, `
		SELECT state FROM control_plane.agent_avatar_upload_reservations
		WHERE operation = 'agent.avatar.upload' AND idempotency_key = 'avatar-atomic-finalize'
	`).Scan(&finalizedState); err != nil || finalizedState != "FINALIZED" {
		t.Fatalf("atomic avatar reservation state=%q err=%v", finalizedState, err)
	}

	objects.afterPut = func(objectstorage.Receipt) {
		if _, updateErr := pool.Exec(ctx, `UPDATE control_plane.agents SET version = version + 1 WHERE ref = $1`, agent.Ref); updateErr != nil {
			t.Errorf("create avatar OCC race: %v", updateErr)
		}
	}
	_, err = upload("avatar-atomic-occ-conflict", updated.Version)
	if !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("avatar OCC race error=%v, want version mismatch", err)
	}
	var conflictState, conflictArtifactRef, conflictObjectKey, conflictObjectVersion string
	var conflictArtifacts int
	if err := pool.QueryRow(ctx, `
		SELECT reservation.state, reservation.artifact_ref, reservation.object_key, reservation.object_version,
		       (SELECT count(*) FROM control_plane.artifacts artifact WHERE artifact.ref = reservation.artifact_ref)
		FROM control_plane.agent_avatar_upload_reservations reservation
		WHERE reservation.operation = 'agent.avatar.upload' AND reservation.idempotency_key = 'avatar-atomic-occ-conflict'
	`).Scan(&conflictState, &conflictArtifactRef, &conflictObjectKey, &conflictObjectVersion, &conflictArtifacts); err != nil {
		t.Fatalf("read compensated avatar reservation: %v", err)
	}
	if conflictState != "COMPENSATED" || conflictArtifactRef == "" || conflictArtifacts != 0 {
		t.Fatalf("avatar OCC compensation state=%q artifact=%q rows=%d", conflictState, conflictArtifactRef, conflictArtifacts)
	}
	if _, err := objects.Head(ctx, conflictObjectKey, conflictObjectVersion); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatalf("avatar OCC compensation left object: %v", err)
	}
	current, err := service.GetAgent(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatalf("read agent after avatar OCC race: %v", err)
	}
	return current
}

func testExpiredAvatarReservationCleanup(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *Repository,
	objects *avatarLifecycleObjectStore,
	owner value.Principal,
	agent entity.Agent,
	projectRef string,
) {
	t.Helper()
	body := avatarPNG(t)
	digest := sha256.Sum256(body)
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve owner for abandoned avatar: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve scope for abandoned avatar: %v", err)
	}
	input := platformrepo.AgentAvatarUpload{
		ArtifactUpload: platformrepo.ArtifactUpload{
			ProjectRef: projectRef, FileName: "abandoned-avatar.png", MediaType: "image/png",
			SizeBytes: int64(len(body)), Digest: "sha256:" + hex.EncodeToString(digest[:]),
			ScanState: "CLEAN", PreviewState: "AVAILABLE", Reader: bytes.NewReader(body),
		},
		AgentRef: agent.Ref, ExpectedVersion: agent.Version,
	}
	mutation := value.Mutation{Operation: "agent.avatar.upload", IdempotencyKey: "avatar-abandoned-expiry", IntentDigest: strings.Repeat("a", 64)}
	reservation, replay, err := repository.reserveAgentAvatarUpload(ctx, current, mutation, input)
	if err != nil || replay != nil {
		t.Fatalf("reserve abandoned avatar: reservation=%#v replay=%#v err=%v", reservation, replay, err)
	}
	receipt, err := repository.putArtifactObject(ctx, reservation.objectKey, input.ArtifactUpload)
	if err != nil {
		t.Fatalf("materialize abandoned avatar: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.agent_avatar_upload_reservations
		SET expires_at = clock_timestamp() - interval '1 minute'
		WHERE ref = $1
	`, reservation.ref); err != nil {
		t.Fatalf("expire abandoned avatar reservation: %v", err)
	}
	if err := repository.CleanupExpiredAgentAvatarUploads(ctx, 10); err != nil {
		t.Fatalf("cleanup abandoned avatar reservation: %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.agent_avatar_upload_reservations WHERE ref = $1`, reservation.ref).Scan(&state); err != nil || state != "COMPENSATED" {
		t.Fatalf("abandoned avatar state=%q err=%v", state, err)
	}
	if _, err := objects.Head(ctx, receipt.Key, receipt.VersionID); !errors.Is(err, objectstorage.ErrNotFound) {
		t.Fatalf("abandoned avatar cleanup left object: %v", err)
	}
}

func createAvatarProject(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, key string) entity.Project {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key},
		Payload: command.ProjectInput{Name: key, Purpose: "Validate avatar lifecycle", Language: "en"},
	})
	if err != nil || result.Project == nil {
		t.Fatalf("create avatar project: project=%#v err=%v", result.Project, err)
	}
	return *result.Project
}

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 28, G: 130, B: 112, A: 255})
	if err := png.Encode(&body, canvas); err != nil {
		t.Fatalf("encode avatar fixture: %v", err)
	}
	return body.Bytes()
}

func uploadAvatarFixture(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, fileName, mediaType string, body []byte, key string) entity.Artifact {
	t.Helper()
	artifact, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: key}, platformrepo.ArtifactUpload{
		ProjectRef: projectRef, FileName: fileName, MediaType: mediaType,
		SizeBytes: int64(len(body)), Reader: bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("upload avatar fixture %s: %v", key, err)
	}
	return artifact
}

func setAvatar(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, agent entity.Agent, artifact entity.Artifact, key string) entity.Agent {
	t.Helper()
	version := agent.Version
	result, err := service.Execute(ctx, command.Command{
		Kind: command.SetAgentAvatar, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
		Payload:  command.AgentAvatarInput{AgentRef: agent.Ref, ArtifactRef: artifact.Ref},
	})
	if err != nil || result.Agent == nil {
		t.Fatalf("set avatar %s: agent=%#v err=%v", key, result.Agent, err)
	}
	return *result.Agent
}

func removeAvatar(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, agent entity.Agent, key string) entity.Agent {
	t.Helper()
	version := agent.Version
	result, err := service.Execute(ctx, command.Command{
		Kind: command.RemoveAgentAvatar, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
		Payload:  command.AgentAvatarInput{AgentRef: agent.Ref},
	})
	if err != nil || result.Agent == nil {
		t.Fatalf("remove avatar %s: agent=%#v err=%v", key, result.Agent, err)
	}
	return *result.Agent
}

func assertRetiredAvatar(t *testing.T, ctx context.Context, pool *pgxpool.Pool, artifactRef, agentRef string) {
	t.Helper()
	var lifecycle string
	var bindings int
	if err := pool.QueryRow(ctx, `
		SELECT artifact.lifecycle_state, count(binding.artifact_id)::integer
		FROM control_plane.artifacts AS artifact
		LEFT JOIN control_plane.artifact_bindings AS binding
		  ON binding.artifact_id = artifact.id
		 AND binding.target_kind = 'AGENT'
		 AND binding.target_ref = $2
		WHERE artifact.ref = $1
		GROUP BY artifact.lifecycle_state
	`, artifactRef, agentRef).Scan(&lifecycle, &bindings); err != nil {
		t.Fatalf("read retired avatar %s: %v", artifactRef, err)
	}
	if lifecycle != "DELETED" || bindings != 0 {
		t.Fatalf("avatar %s left an orphan binding: lifecycle=%s bindings=%d", artifactRef, lifecycle, bindings)
	}
}

func assertAvatarUpdateError(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, agent entity.Agent, avatarURL, key string, expected error) {
	t.Helper()
	version := agent.Version
	_, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateAgent, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
		Payload: command.AgentInput{
			Ref: agent.Ref, Name: agent.Name, Purpose: agent.Purpose, RoleDescription: agent.RoleDescription,
			RoleDefinitionRef: agent.RoleDefinitionRef, AvatarURL: avatarURL, RuntimeRef: agent.RuntimeKey,
		},
	})
	if !errors.Is(err, expected) {
		t.Fatalf("unsafe avatar %q returned %v, want %v", avatarURL, err, expected)
	}
}

func deleteAvatarArtifact(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, artifact entity.Artifact, key string) {
	t.Helper()
	impact, err := service.GetArtifactImpact(ctx, owner, artifact.Ref, "DELETE")
	if err != nil || !impact.Permitted {
		t.Fatalf("preflight avatar artifact delete: impact=%#v err=%v", impact, err)
	}
	version := artifact.Version
	result, err := service.Execute(ctx, command.Command{
		Kind: command.DeleteArtifact, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
		Payload: command.ArtifactLifecycleInput{ArtifactRef: artifact.Ref, ImpactDigest: impact.Digest},
	})
	if err != nil || result.Artifact == nil || result.Artifact.LifecycleState != "DELETED" {
		t.Fatalf("soft-delete avatar artifact: artifact=%#v err=%v", result.Artifact, err)
	}
}
