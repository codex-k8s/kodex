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

// avatarLifecycleObjectStore wraps the in-memory versioned object fixture with an optional
// one-shot afterPut hook for controlled OCC/permission races. It stores synthetic objects only and
// does not represent a live object-store adapter.
// avatarLifecycleObjectStore оборачивает memory fixture версионированных объектов одноразовым
// afterPut hook для управляемых OCC и permission races. Хранит только синтетические объекты, не
// представляет живой storage adapter.
type avatarLifecycleObjectStore struct {
	*objectstoragetest.Store
	afterPut func(objectstorage.Receipt)
}

// Put delegates to the fixture store and invokes a successful-write callback once, clearing it
// before invocation to prevent recursive uploads from reusing the hook. The original receipt/error
// is returned unchanged.
// Put делегирует запись fixture store и однократно вызывает callback после успеха, очищая hook до
// вызова ради рекурсивных загрузок. Исходные receipt и error сохраняются.
func (store *avatarLifecycleObjectStore) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	receipt, err := store.Store.Put(ctx, input)
	if err == nil && store.afterPut != nil {
		callback := store.afterPut
		store.afterPut = nil
		callback(receipt)
	}
	return receipt, err
}

// TestAvatarLifecycleComponent uses the isolated PostgreSQL contour plus synthetic
// storage/scanner/provider/image metadata to exercise avatar upload/replay/OCC compensation,
// expiry cleanup, attachment rejection, immutable revisions, replacement/removal and fresh receipt
// authority. It skips when that contour is absent and never proves live object storage, antivirus,
// Kubernetes or browser acceptance.
// TestAvatarLifecycleComponent использует isolated PostgreSQL и синтетические storage, scanner,
// provider и image metadata для upload, replay, OCC compensation, expiry cleanup, rejection,
// immutable revisions, replacement, removal и fresh receipt authority. Без контура пропускается;
// не доказывает живые storage, antivirus, Kubernetes и browser acceptance.
func TestAvatarLifecycleComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
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
	configureComponentArtifactScanner(t, repository)
	configureComponentProviderAndRoleImages(t, repository)
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
	testUploadReceiptFreshAuthority(t, ctx, repository, service, objects, owner, updated, primary.Ref)
}

// testUploadReceiptFreshAuthority creates narrow actor bindings, proves initial avatar/artifact
// replay, revokes permissions and requires hidden NotFound with empty results on later access. A
// one-shot storage callback also creates a receipt/revocation race before outer finalization. It
// relies on its caller disposable database and synthetic objects.
// testUploadReceiptFreshAuthority создаёт узкие bindings субъекта, проверяет avatar и artifact
// replay, отзывает права и требует скрытый NotFound с пустым результатом. Одноразовый storage
// callback также создаёт receipt/revocation race до внешнего finalize. Использует disposable БД
// вызывающего и синтетические объекты.
func testUploadReceiptFreshAuthority(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, objects *avatarLifecycleObjectStore, owner value.Principal, agent entity.Agent, projectRef string) {
	t.Helper()
	reader := contextProjectReader(t, ctx, repository, service, owner, projectRef, "UPLOAD_RECEIPT")
	actor, err := repository.ResolvePrincipal(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}
	fresh := func() value.Principal {
		return receiptFreshPrincipal(t, ctx, repository, projectRef)
	}
	revoke := func(binding entity.AccessBinding, key string) {
		if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeAccessBinding, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &binding.Version}, Payload: command.AccessBindingInput{BindingRef: binding.Ref}}); err != nil {
			t.Fatal(err)
		}
	}
	avatarBinding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, "avatar-receipt-access", []string{"agent.avatar.manage", "agent.view"}, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "AGENT", ResourceRef: agent.Ref})
	body := avatarPNG(t)
	uploadAvatar := func() (entity.Agent, error) {
		return service.UploadAgentAvatar(ctx, fresh(), value.Mutation{IdempotencyKey: "avatar-receipt-protected"}, platformrepo.AgentAvatarUpload{ArtifactUpload: platformrepo.ArtifactUpload{ProjectRef: projectRef, FileName: "receipt.png", MediaType: "image/png", SizeBytes: int64(len(body)), Reader: bytes.NewReader(body)}, AgentRef: agent.Ref, ExpectedVersion: agent.Version})
	}
	first, err := uploadAvatar()
	if err != nil || first.Ref != agent.Ref {
		t.Fatalf("authorized avatar upload: %v", err)
	}
	if replay, err := uploadAvatar(); err != nil || replay.Version != first.Version {
		t.Fatalf("authorized avatar replay: %v", err)
	}
	revoke(avatarBinding, "avatar-receipt-revoke")
	if result, err := uploadAvatar(); !errors.Is(err, domainerrs.ErrNotFound) || result.Ref != "" {
		t.Fatalf("revoked avatar receipt escaped fresh permission: %v", err)
	}
	for _, racing := range []bool{false, true} {
		key := "artifact-receipt-preflight"
		if racing {
			key = "artifact-receipt-racing"
		}
		binding := receiptAccessBinding(t, ctx, service, owner, actor.ActorID, key, []string{"artifact.upload", "artifact.view"}, entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef})
		upload := func() (entity.Artifact, error) {
			return service.UploadArtifact(ctx, fresh(), value.Mutation{IdempotencyKey: key}, platformrepo.ArtifactUpload{ProjectRef: projectRef, FileName: key + ".txt", MediaType: "text/plain", SizeBytes: 4, Reader: bytes.NewReader([]byte("data"))})
		}
		if racing {
			objects.afterPut = func(_ objectstorage.Receipt) {
				if _, err := upload(); err != nil {
					t.Fatalf("concurrent artifact receipt: %v", err)
				}
				revoke(binding, key+"-revoke")
			}
		} else {
			first, err := upload()
			if err != nil {
				t.Fatal(err)
			}
			if replay, err := upload(); err != nil || replay.Ref != first.Ref {
				t.Fatalf("authorized artifact receipt: %v", err)
			}
			revoke(binding, key+"-revoke")
		}
		if result, err := upload(); !errors.Is(err, domainerrs.ErrNotFound) || result.Ref != "" {
			t.Fatalf("revoked artifact receipt (%s): %v", key, err)
		}
	}
}

// testAtomicAvatarUpload requires one avatar version advance and stable exact replay, then changes
// the agent version after object Put to force OCC failure. It verifies COMPENSATED state, no
// artifact row and authoritative object absence before returning the current agent from the caller
// component contour.
// testAtomicAvatarUpload требует один шаг версии avatar и точный стабильный replay, затем меняет
// версию агента после Put ради OCC failure. Проверяет COMPENSATED, отсутствие artifact row и
// объекта до возврата текущего агента из component-контура.
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

// testExpiredAvatarReservationCleanup creates a server reservation and materialized fixture object
// without finalization, expires the row explicitly in disposable PostgreSQL, then requires bounded
// cleanup to persist COMPENSATED and remove the exact object version. It simulates an abandoned
// upload, not elapsed wall time or a live cleanup worker.
// testExpiredAvatarReservationCleanup создаёт reservation и fixture object без finalize, явно
// истекает строку disposable PostgreSQL и требует bounded cleanup с COMPENSATED и отсутствием
// точной object version. Моделирует брошенную загрузку, не реальное ожидание и не живой cleanup
// worker.
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

// createAvatarProject creates one synthetic English-language project through the real component
// service command with the supplied idempotency key and requires a project result. It does not
// bypass principal/command validation or create a live environment.
// createAvatarProject создаёт синтетический англоязычный проект через service command контура с
// заданным idempotency key и требует project result. Не обходит principal и command validation и
// не создаёт живую среду.
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

// avatarPNG encodes a deterministic two-by-two RGBA image into PNG bytes for upload fixtures,
// failing the test on encoding error. No filesystem or external image service is used.
// avatarPNG кодирует детерминированное RGBA изображение 2×2 в PNG для fixture загрузок, завершая
// тест при ошибке кодирования. Файлы и внешний image service не используются.
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

// uploadAvatarFixture uploads caller-supplied synthetic bytes through the component service with
// exact project/name/media/size and a new bytes reader, returning the artifact or failing the
// test. Digest and upload authority are resolved by the service path.
// uploadAvatarFixture загружает синтетические байты через component service с точными project,
// name, media и size и новым bytes reader, возвращая artifact либо завершая тест. Digest и upload
// authority разрешает service path.
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

// setAvatar issues the specialized SetAgentAvatar command with the current agent version and exact
// artifact ref, requiring an agent result. It exercises caller contour OCC/authorization rather
// than writing avatar metadata directly.
// setAvatar вызывает SetAgentAvatar с текущей версией агента и точной artifact ref и требует agent
// result. Проверяет OCC и authorization контура, не пишет avatar metadata напрямую.
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

// removeAvatar issues RemoveAgentAvatar with the current version and caller idempotency key,
// requiring the returned agent. Fallback and prior artifact retirement are asserted separately by
// the component caller.
// removeAvatar вызывает RemoveAgentAvatar с текущей версией и idempotency key, требуя
// возвращённого агента. Fallback и retirement прежнего artifact отдельно проверяет component
// caller.
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

// assertRetiredAvatar reads the selected artifact lifecycle and exact agent binding count from
// disposable PostgreSQL and requires DELETED with zero bindings. This checks logical retirement
// only, not physical object deletion.
// assertRetiredAvatar читает lifecycle выбранного artifact и число bindings точного агента из
// disposable PostgreSQL, требуя DELETED и ноль bindings. Проверяет логический retirement, не
// физическое удаление объекта.
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

// assertAvatarUpdateError submits an ordinary UpdateAgent containing the proposed avatar URL and
// current version, preserving the other agent fields, then requires the specified rejection. It
// checks that unsafe URL assignment cannot bypass the specialized avatar flow.
// assertAvatarUpdateError передаёт обычному UpdateAgent предложенный avatar URL и текущую версию,
// сохраняя остальные поля, затем требует заданный отказ. Проверяет невозможность обхода
// специализированного avatar flow через небезопасный URL.
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

// deleteAvatarArtifact obtains the caller contour DELETE impact, then soft-deletes at exact
// artifact version and impact digest and requires DELETED metadata. Permitted is a preflight hint,
// while the command remains authoritative; no physical purge is executed here.
// deleteAvatarArtifact получает DELETE impact контура, затем soft-delete точных artifact version и
// impact digest и требует DELETED metadata. Permitted — предварительная подсказка, команда
// остаётся авторитетной; физический purge здесь не выполняется.
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
