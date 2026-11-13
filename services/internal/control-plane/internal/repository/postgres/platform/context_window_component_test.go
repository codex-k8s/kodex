package platform

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// testContextWindowPublication exercises real owner commands with synthetic admitted image evidence.
// It checks unsupported bootstrap, numeric/model bounds, immutable publication/rollback, selected-image preflight and tenant/hash denial.
// It neither builds an image nor calls a hosted model; the surrounding promotion fixture provides the full owner artifact lifecycle.
// testContextWindowPublication проверяет настоящие команды владельца с синтетическим evidence допущенного образа.
// Проверяет неподдержанный bootstrap, пределы модели/числа, неизменяемую публикацию/откат, preflight выбранного образа и отказы tenant/hash.
// Не собирает образ и не обращается к размещённой модели; внешний promotion fixture задаёт полный жизненный цикл artifact владельца.
func testContextWindowPublication(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, projectRef string, artifact entity.ImageArtifact) {
	t.Helper()
	seedObservedCatalogFixture(t, ctx, r)
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef, "context-window-agent", "Context window specialist")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	initial := view.PublishedOverlay
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapArtifact, err := scanRoleImageArtifact(r.pool.QueryRow(ctx, queryRoleImagesGetActiveArtifact, ownerScope.organizationID, view.Environment.CurrentVersion.Image.ArtifactRef))
	if err != nil {
		t.Fatal(err)
	}
	syntheticRaw, _ := imageInventoryFixture(bootstrapArtifact)
	synthetic, err := runtimecontract.DecodeImageToolInventory([]byte(syntheticRaw))
	if err != nil {
		t.Fatal(err)
	}
	synthetic.Platforms[0].Manifest.InputFeatures = []string{runtimecontract.ContextWindowInputFeature}
	manifestRaw, _ := json.Marshal(synthetic.Platforms[0].Manifest)
	synthetic.Platforms[0].ManifestSHA256 = runtimecontract.ImageInventorySHA256(manifestRaw)
	syntheticBytes, _ := json.Marshal(synthetic)
	var syntheticRef string
	if err := r.pool.QueryRow(ctx, queryAssistantAgentRuntimeInventoryFixture, pgx.StrictNamedArgs{"organization_id": ownerScope.organizationID, "artifact_ref": bootstrapArtifact.Ref, "manifest_digest": bootstrapArtifact.ManifestDigest, "provenance_sha256": bootstrapArtifact.ProvenanceSHA256, "inventory_json": string(syntheticBytes), "inventory_sha256": runtimecontract.ImageInventorySHA256(syntheticBytes)}).Scan(&syntheticRef); err != nil || syntheticRef != bootstrapArtifact.Ref {
		t.Fatal("seed synthetic bootstrap input feature", err)
	}
	bootstrap := view.EnvironmentBinding
	sequence := 0
	invoke := func(kind command.Kind, content, ref string) {
		t.Helper()
		sequence++
		version := view.AgentVersion
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-window-" + strconvFormat(int64(sequence)), ExpectedVersion: &version}, Payload: command.ConfigOverlayInput{AgentRef: agent.Ref, Content: content, PublishedOverlayRef: ref}})
		if err != nil || result.RuntimeConfiguration == nil {
			t.Fatalf("context window owner command: %v", err)
		}
		view = *result.RuntimeConfiguration
	}
	invoke(command.CreateConfigOverlayDraft, "[context]\nwindow_tokens = 16384\n", "")
	invoke(command.ValidateConfigOverlayDraft, "", "")
	if view.DraftOverlay.State != "INVALID" || len(view.DraftOverlay.ValidationMessages) != 1 || view.DraftOverlay.ValidationMessages[0] != "i18n:CONTEXT_WINDOW_IMAGE_UNSUPPORTED" {
		t.Fatal("bootstrap image accepted optional input")
	}
	unsupportedVersion := view.AgentVersion
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishConfigOverlayDraft, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-window-unsupported-publish", ExpectedVersion: &unsupportedVersion}, Payload: command.ConfigOverlayInput{AgentRef: agent.Ref}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unsupported image published")
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-window-environment"}, Payload: command.RuntimeEnvironmentInput{ProjectRef: projectRef, Name: "Context window image", ImageArtifactRef: artifact.Ref}})
	if err != nil || created.RuntimeEnvironment == nil {
		t.Fatalf("create supported environment: %v", err)
	}
	version := view.AgentVersion
	bound, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-window-bind", ExpectedVersion: &version}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: created.RuntimeEnvironment.Ref, VersionRef: created.RuntimeEnvironment.CurrentVersion.Ref}})
	if err != nil || bound.RuntimeConfiguration == nil {
		t.Fatalf("bind supported image: %v", err)
	}
	view = *bound.RuntimeConfiguration
	for _, content := range []string{"[context]\nwindow_tokens = 0", "[context]\nwindow_tokens = 2000001", "[context]\nwindow_tokens = 9223372036854775808", "[context]\nwindow_tokens = 1e4", "[context]\nwindow_tokens = 1.5", "[context]\nwindow_tokens = 32769"} {
		invoke(command.CreateConfigOverlayDraft, content, "")
		invoke(command.ValidateConfigOverlayDraft, "", "")
		if view.DraftOverlay.State != "INVALID" || view.PublishedOverlay.Ref != initial.Ref {
			t.Fatal("invalid context changed publication")
		}
	}
	invoke(command.CreateConfigOverlayDraft, "[context]\nwindow_tokens = 16384\n", "")
	invoke(command.ValidateConfigOverlayDraft, "", "")
	if view.DraftOverlay.State != "VALID" {
		t.Fatalf("supported budget not valid: %v", view.DraftOverlay.ValidationMessages)
	}
	invoke(command.PublishConfigOverlayDraft, "", "")
	published := view.PublishedOverlay
	if !strings.Contains(view.SafeEffectiveConfig, "WindowTokens = 16384") || !strings.Contains(view.SafeEffectiveConfig, "CatalogWindowTokens = 32768") {
		t.Fatal("effective preview lost requested/catalog window")
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requireCurrentRuntimeOverlayImageFeature(ctx, tx, current.organizationID, agent.Ref, published.Content); err != nil {
		t.Fatal("selected supported image failed preflight", err)
	}
	for _, item := range []struct{ org, ref, digest string }{{current.organizationID, artifact.Ref, "sha256:" + strings.Repeat("0", 64)}, {"90000000-0000-4000-8000-000000000099", artifact.Ref, artifact.ManifestDigest}, {current.organizationID, "", artifact.ManifestDigest}} {
		if err := requireRuntimeOverlayImageFeature(ctx, tx, item.org, item.ref, item.digest, published.Content); !errors.Is(err, errContextWindowImageUnsupported) {
			t.Fatalf("foreign/missing image support accepted: %v", err)
		}
	}
	if err := requireRuntimeOverlayImageFeature(ctx, tx, current.organizationID, "", "", ""); err != nil {
		t.Fatal("legacy unset required inventory")
	}
	_ = tx.Rollback(ctx)
	version = view.AgentVersion
	if _, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "context-window-unsupported-rebind", ExpectedVersion: &version}, Payload: command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: bootstrap.EnvironmentRef, VersionRef: bootstrap.VersionRef}}); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("explicit budget rebound to bootstrap")
	}
	unchanged, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || unchanged.AgentVersion != version || unchanged.EnvironmentBinding != view.EnvironmentBinding {
		t.Fatal("rejected binding partially changed owner state")
	}
	invoke(command.RollbackConfigOverlay, "", initial.Ref)
	if view.PublishedOverlay.Ref == initial.Ref || view.PublishedOverlay.Content != initial.Content {
		t.Fatal("rollback did not publish fresh inherit revision")
	}
	exact, err := service.GetConfigOverlayRevision(ctx, owner, agent.Ref, published.Ref)
	if err != nil || exact.Content != published.Content || exact.Digest != published.Digest {
		t.Fatal("context publication history changed")
	}
	if !artifact.ToolInventory.SupportsInputFeature(runtimecontract.ContextWindowInputFeature) {
		t.Fatal("fixture missing admitted feature proof")
	}
}

// TestContextWindowComponent qualifies optional image support through real promotion, owner publication and rollback in a disposable database.
// TestContextWindowComponent проверяет необязательную поддержку образа через настоящие promotion, публикацию владельца и откат в одноразовой БД.
func TestContextWindowComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	r, err := New(pool, "opencode-synthetic", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentProviderAndRoleImages(t, r)
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	testRoleImagePromotionScenario(t, ctx, r, true)
}
