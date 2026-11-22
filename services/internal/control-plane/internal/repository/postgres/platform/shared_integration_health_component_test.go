package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// querySharedHealthTestRead compares actual owner snapshots with the former TOKEN/NONE JSON shape and reads no material or generic Secret locator into diagnostics.
// querySharedHealthTestRead сравнивает actual owner snapshots с прежней TOKEN/NONE JSON shape, не читая material либо generic Secret locator в diagnostics.
//
//go:embed testdata/sql/shared_health_test__read.sql
var querySharedHealthTestRead string

// TestSharedIntegrationHealthOwnerComponent exercises actual public queue/claim/completion for NONE and TOKEN over disposable PostgreSQL, preserving old snapshot bytes and typed credential absence; synthetic completion is not provider or installed readiness.
// TestSharedIntegrationHealthOwnerComponent проверяет actual public queue/claim/completion NONE и TOKEN в disposable PostgreSQL, сохраняя old snapshot bytes и typed credential absence; synthetic completion не является provider либо installed readiness.
func TestSharedIntegrationHealthOwnerComponent(t *testing.T) {
	for _, test := range []struct {
		definition, kind string
		configuration    map[string]any
	}{
		{"synthetic", "NONE", map[string]any{"journal": "shared_health_none"}},
		{"context7", "TOKEN", map[string]any{"base_url": "https://mcp.context7.com"}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal("open isolated shared health database")
			}
			defer pool.Close()
			repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
			if err := repository.Bootstrap(ctx); err != nil {
				t.Fatal("bootstrap shared health owner", err)
			}
			service, err := platformservice.New(repository)
			if err != nil {
				t.Fatal(err)
			}
			owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
			gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-tests.claim"}, "integration-gateway")
			current, err := repository.ResolvePrincipal(ctx, owner)
			if err != nil {
				t.Fatal(err)
			}
			scope, err := repository.resolveScope(ctx, current)
			if err != nil {
				t.Fatal(err)
			}
			created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "shared-health-create-" + test.kind}, Payload: command.ConnectionInput{DefinitionKey: test.definition, Name: "Disposable shared health", PublicConfiguration: test.configuration}})
			if err != nil || created.Connection == nil {
				t.Fatal("create exact ordinary shared Connection", err)
			}
			started, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "shared-health-test-" + test.kind, ExpectedVersion: &created.Connection.Version}, Payload: command.ConnectionInput{Ref: created.Connection.Ref}})
			if err != nil || started.Connection == nil || started.Connection.State != "TESTING" {
				t.Fatal("queue shared owner test", err)
			}
			var ref, kind, purpose, identity, snapshot, digest, state string
			var legacyEqual bool
			read := func() {
				t.Helper()
				if err := pool.QueryRow(ctx, querySharedHealthTestRead, scope.organizationID, created.Connection.Ref).Scan(&ref, &kind, &purpose, &identity, &snapshot, &digest, &state, &legacyEqual); err != nil {
					t.Fatal("read actual shared owner snapshot", err)
				}
			}
			read()
			sum := sha256.Sum256([]byte(snapshot))
			if kind != test.kind || purpose != "" || identity != "" || state != "DUE" || !legacyEqual || digest != hex.EncodeToString(sum[:]) {
				t.Fatal("ordinary shared snapshot changed representation or bytes")
			}
			var object map[string]any
			if json.Unmarshal([]byte(snapshot), &object) != nil {
				t.Fatal("decode shared snapshot")
			}
			if _, present := object["brokerIdentityRef"]; present {
				t.Fatal("ordinary snapshot acquired broker metadata")
			}
			before, originalDigest := snapshot, digest
			claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "shared-health-gateway", 32)
			if err != nil {
				t.Fatal("claim ordinary shared owner test", err)
			}
			var claim map[string]any
			for _, candidate := range claims {
				if candidate["testRef"] == ref {
					claim = candidate
					break
				}
			}
			if claim == nil || claim["credentialDeliveryKind"] != test.kind || claim["inputDigest"] != digest || claim["credential"] != nil || claim["brokerIdentityRef"] != nil {
				t.Fatal("typed shared claim invented credential material")
			}
			result, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "shared-health-complete-" + test.kind}, Payload: command.IntegrationConnectionTestInput{TestRef: ref, LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: false, SafeErrorCode: "INTEGRATION_UNAVAILABLE"}})
			if err != nil || result.Connection == nil {
				t.Fatal("complete synthetic ordinary shared test", err)
			}
			read()
			if state != "FAILED" || snapshot != before || digest != originalDigest || kind != test.kind {
				t.Fatal("terminal owner receipt rewrote immutable input")
			}
			definition := repository.integrationDefinitions[test.definition]
			delivery, known := definition.ConnectionCredentialDelivery()
			if !known || delivery.Kind != integrationpackage.CredentialDeliveryKind(test.kind) {
				t.Fatal("claim did not retain exact package representation")
			}
		})
	}
}
