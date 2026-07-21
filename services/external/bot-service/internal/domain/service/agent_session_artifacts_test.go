package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestAgentSessionArtifactsAreBoundToExactActiveRun(t *testing.T) {
	store, runner, publisher := agentSessionStatusTestDeps()
	store.botIdentities = map[int64]entity.MattermostBotIdentity{1: {
		ID: 1, ProjectID: 1, RoleID: 1, MattermostUserID: "role-user", TokenSecretRef: "role-token-secret", Status: "active",
	}}
	artifacts := &fakeAgentSessionArtifacts{}
	svc := NewAgentSessionService(AgentSessionServiceConfig{
		Localizer: testLocalizer(t, texti18n.DefaultLocale), Store: store, RuntimeRunner: runner, ThreadPublisher: publisher,
		StorageReady: true, RuntimeReady: true, Artifacts: artifacts,
	})
	snapshot, err := svc.Snapshot(context.Background(), "session-1", "session-token")
	if err != nil || !snapshot.ArtifactsEnabled {
		t.Fatalf("Snapshot() = %#v, error=%v", snapshot, err)
	}
	claim, err := svc.ClaimNextTurn(context.Background(), "session-1", "session-token")
	if err != nil || !claim.HasTurn || claim.ArtifactManifest.TurnID != "run-1" || artifacts.manifestScope.SessionKey != "session-1" {
		t.Fatalf("ClaimNextTurn() = %#v, scope=%#v error=%v", claim, artifacts.manifestScope, err)
	}
	if _, _, err := svc.DownloadArtifact(context.Background(), "session-1", "session-token", "run-other", strings.Repeat("a", 32)); !errors.Is(err, domainartifact.ErrScopeDenied) {
		t.Fatalf("foreign turn download error = %v", err)
	}
	version, body, err := svc.DownloadArtifact(context.Background(), "session-1", "session-token", "run-1", strings.Repeat("a", 32))
	if err != nil {
		t.Fatalf("DownloadArtifact() error = %v", err)
	}
	opened, _ := io.ReadAll(body)
	_ = body.Close()
	if version.Scope.TurnID != "run-1" || string(opened) != "inbound" {
		t.Fatalf("download version=%#v body=%q", version, opened)
	}
	if _, err := svc.PublishArtifact(context.Background(), "session-1", "session-token", PublishAgentSessionArtifactCommand{
		TurnID: "run-other", IdempotencyKey: "answer-v1", OriginalName: "answer.txt", Body: strings.NewReader("answer"),
	}); !errors.Is(err, domainartifact.ErrScopeDenied) {
		t.Fatalf("foreign turn publish error = %v", err)
	}
	result, err := svc.PublishArtifact(context.Background(), "session-1", "session-token", PublishAgentSessionArtifactCommand{
		TurnID: "run-1", IdempotencyKey: "answer-v1", OriginalName: "answer.txt", Body: strings.NewReader("answer"),
	})
	if err != nil || result.State != domainartifact.DeliveryDelivered || artifacts.publishInput.BotTokenSecretRef != "role-token-secret" || artifacts.publishedBody != "answer" {
		t.Fatalf("PublishArtifact() result=%#v input=%#v body=%q error=%v", result, artifacts.publishInput, artifacts.publishedBody, err)
	}
}

type fakeAgentSessionArtifacts struct {
	manifestScope domainartifact.Scope
	publishInput  domainartifact.PublishInput
	publishedBody string
}

func (service *fakeAgentSessionArtifacts) ManifestForTurn(_ context.Context, scope domainartifact.Scope) (domainartifact.Manifest, error) {
	service.manifestScope = scope
	return domainartifact.Manifest{SchemaVersion: domainartifact.ManifestSchemaVersion, TurnID: scope.TurnID, Files: []domainartifact.ManifestEntry{}}, nil
}

func (service *fakeAgentSessionArtifacts) OpenForTurn(_ context.Context, scope domainartifact.Scope, versionID string) (domainartifact.Version, io.ReadCloser, error) {
	if versionID != strings.Repeat("a", 32) {
		return domainartifact.Version{}, nil, domainartifact.ErrNotFound
	}
	return domainartifact.Version{VersionID: versionID, Scope: scope, State: domainartifact.StateAvailable, Size: 7, SHA256: strings.Repeat("b", 64)}, io.NopCloser(bytes.NewReader([]byte("inbound"))), nil
}

func (service *fakeAgentSessionArtifacts) PublishOutgoing(_ context.Context, input domainartifact.PublishInput) (domainartifact.PublishResult, error) {
	service.publishInput = input
	body, _ := io.ReadAll(input.Body)
	service.publishedBody = string(body)
	return domainartifact.PublishResult{ArtifactVersionID: strings.Repeat("c", 32), DeliveryID: strings.Repeat("d", 32), State: domainartifact.DeliveryDelivered}, nil
}
