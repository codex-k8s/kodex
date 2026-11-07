package platform

import (
	"context"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAtlassianServerDefinitionsComponent checks startup registration and durable connection pins in disposable PostgreSQL.
// It verifies explicit context and Server scope configuration without creating credentials, grants or provider effects.
// TestAtlassianServerDefinitionsComponent проверяет startup registration и durable connection pins в disposable PostgreSQL.
// Проверяет явные context и Server scope без создания credentials, grants и provider effects.
func TestAtlassianServerDefinitionsComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated Server profile PostgreSQL")
	}
	defer pool.Close()
	repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	owner := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	service, err := serviceplatform.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"jira-server-dc", "confluence-server-dc"} {
		t.Run(key, func(t *testing.T) {
			definition, exists := repository.integrationDefinitions[key]
			expectedVersion := "1.0.0"
			if key == "jira-server-dc" {
				expectedVersion = "1.1.0"
			}
			if !exists || definition.Metadata.Version != expectedVersion {
				t.Fatal("Server definition missing from owner registry")
			}
			for _, contextPath := range []string{"/", "/suite"} {
				configuration := map[string]any{"base_url": "https://api.example.invalid", "context_path": contextPath, "auth_scheme": "BEARER"}
				if key == "jira-server-dc" {
					configuration["project_key"], configuration["issue_type"] = "DEMO", "Task"
				} else {
					configuration["space_key"] = "DEMO"
				}
				created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "server-connection-" + key + contextPath},
					Payload:  command.ConnectionInput{DefinitionKey: key, Name: "Synthetic " + key + contextPath, PublicConfiguration: configuration}})
				if err != nil || created.Connection == nil {
					t.Fatalf("Server connection creation failed for context %q: %v", contextPath, err)
				}
				observed, err := service.GetIntegrationConnection(ctx, owner, created.Connection.Ref)
				if err != nil || observed.DefinitionKey != key || observed.DefinitionVersion != expectedVersion ||
					observed.DefinitionDigest != definition.Digest || observed.PublicConfiguration["context_path"] != contextPath ||
					observed.CredentialRevision != nil || len(observed.Grants) != 0 || observed.State != "NOT_CONNECTED" {
					t.Fatalf("Server connection lost its exact pins or inherited access: %v", err)
				}
				for _, capability := range definition.Spec.Capabilities {
					configurationStrings := make(map[string]string)
					for name, value := range observed.PublicConfiguration {
						configurationStrings[name] = value.(string)
					}
					scope, err := capability.ResourceScopeValues(configurationStrings)
					if err != nil || scope["context_path"] != contextPath {
						t.Fatal("persisted configuration omitted the context scope")
					}
				}
				delete(configuration, "context_path")
				invalid, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "server-missing-context-" + key + contextPath},
					Payload:  command.ConnectionInput{DefinitionKey: key, Name: "Invalid Server profile", PublicConfiguration: configuration}})
				if err == nil || invalid.Connection != nil {
					t.Fatal("Server connection accepted an implicit context")
				}
			}
		})
	}
}
