package platform

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         strings.Repeat("e", 64),
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
