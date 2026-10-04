package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Матрица использует только отдельную мигрированную disposable БД. Completion
// синтетический: проверяем owner ledger и fences, не выдаём его за live probe.
func TestIntegrationHealthSnapshotComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated health snapshot PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.command.integrations.create", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	gateway := principal("integration-gateway", "platform.runtime.integration-tests.claim", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "health-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("synthetic health command %s failed: %v", kind, err)
		}
		return result
	}
	connection := func(key string) entity.IntegrationConnection {
		t.Helper()
		created := execute(command.CreateConnection, key+"-create", nil, command.ConnectionInput{DefinitionKey: "context7", Name: "Synthetic health " + key, PublicConfiguration: map[string]any{"base_url": "https://mcp.context7.com"}}).Connection
		if created == nil {
			t.Fatal("connection missing")
		}
		// Только synthetic credential metadata: ни ключа, ни Secret чтения.
		_, err := pool.Exec(ctx, `WITH credential AS (
		INSERT INTO control_plane.integration_credential_revisions(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
		SELECT 'icr_health_'||ref,organization_id,id,1,'kodex-system/synthetic#api_key','60000000-0000-4000-8000-000000000001'::uuid,'1',repeat('a',64),created_by
		FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
		UPDATE control_plane.integration_connections c SET credential_revision_id=credential.id,state='CONNECTED',masked_credentials_state='CONFIGURED',version=version+1 FROM credential WHERE c.id=credential.connection_id`, created.Ref)
		if err != nil {
			t.Fatal("prepare synthetic health credential metadata")
		}
		fresh, err := service.GetIntegrationConnection(ctx, owner, created.Ref)
		if err != nil {
			t.Fatal(err)
		}
		return fresh
	}
	start := func(key string, c entity.IntegrationConnection) (entity.IntegrationConnection, string) {
		t.Helper()
		tested := execute(command.TestConnection, key+"-start", &c.Version, command.ConnectionInput{Ref: c.Ref}).Connection
		var ref string
		if err := pool.QueryRow(ctx, `SELECT ref FROM control_plane.integration_connection_tests WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1) AND state='DUE'`, c.Ref).Scan(&ref); err != nil {
			t.Fatal("health test not created")
		}
		return *tested, ref
	}
	claim := func(ref string) map[string]any {
		t.Helper()
		claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "health-fixture", 32)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range claims {
			if stringMap(item, "connectionRef") == ref {
				return item
			}
		}
		return nil
	}
	complete := func(key string, item map[string]any, payload command.IntegrationConnectionTestInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "health-" + key}, Payload: payload})
	}
	payload := func(item map[string]any) command.IntegrationConnectionTestInput {
		return command.IntegrationConnectionTestInput{TestRef: stringMap(item, "testRef"), LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: item["generation"].(int64), Success: true}
	}
	mutateConfig := func(ref string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET public_configuration=public_configuration||'{"changed":true}'::jsonb,version=version+1 WHERE ref=$1`, ref); err != nil {
			t.Fatal("mutate isolated health configuration")
		}
	}

	t.Run("immutable capture and stale before claim", func(t *testing.T) {
		c, testRef := start("capture", connection("capture"))
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT input_snapshot FROM control_plane.integration_connection_tests WHERE ref=$1`, testRef).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var snapshot map[string]any
		if json.Unmarshal(raw, &snapshot) != nil || snapshot["connectionRef"] != c.Ref || snapshot["connectionVersion"] != float64(c.Version) || snapshot["definitionDigest"] != c.DefinitionDigest || snapshot["credentialRevision"] != float64(1) {
			t.Fatal("create did not capture exact health input")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET input_snapshot='{}'::jsonb WHERE ref=$1`, testRef); err == nil {
			t.Fatal("health snapshot changed")
		}
		mutateConfig(c.Ref)
		if claim(c.Ref) != nil {
			t.Fatal("stale health input was claimed")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET state='CLAIMED',lease_ref='lea_snapshot01',fence_digest=repeat('a',64),generation=1,workload_instance='fixture',lease_expires_at=clock_timestamp()+INTERVAL '30 seconds' WHERE ref=$1`, testRef); err == nil {
			t.Fatal("stale health snapshot passed update barrier")
		}
	})
	t.Run("claim fences and stale completion", func(t *testing.T) {
		c, _ := start("fences", connection("fences"))
		item := claim(c.Ref)
		if item == nil {
			t.Fatal("exact health input was not claimed")
		}
		for _, kind := range []string{"generation", "fence", "lease"} {
			bad := payload(item)
			switch kind {
			case "generation":
				bad.Generation++
			case "fence":
				bad.Fence = "fnc_foreign01"
			case "lease":
				bad.LeaseRef = "lea_foreign01"
			}
			if _, err := complete("bad-"+kind, item, bad); !errors.Is(err, errs.ErrForbidden) {
				t.Fatal("foreign claim pins accepted")
			}
		}
		mutateConfig(c.Ref)
		if _, err := complete("stale-completion", item, payload(item)); err == nil {
			t.Fatal("changed configuration completion became ready")
		}
		var state string
		if err := pool.QueryRow(ctx, `SELECT state FROM control_plane.integration_connection_tests WHERE ref=$1`, stringMap(item, "testRef")).Scan(&state); err != nil || state != "CLAIMED" {
			t.Fatal("failed completion published terminal")
		}
	})
	t.Run("fresh receipt survives grant-only change", func(t *testing.T) {
		c, _ := start("fresh", connection("fresh"))
		item := claim(c.Ref)
		if item == nil {
			t.Fatal("fresh health was not claimed")
		}
		finished, err := complete("fresh-complete", item, payload(item))
		if err != nil || finished.Connection == nil {
			t.Fatal("complete exact health receipt failed")
		}
		c = *finished.Connection
		project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Health snapshot fixture", Language: "en"}).Project
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "health-agent", "Health reader")
		for _, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
			c = *execute(command.ChangeIntegrationGrant, capability, &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: capability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
		}
		readProof := func(c entity.IntegrationConnection) ([]runtimecontract.ManagedMCPProfile, error) {
			grants := []runtimecontract.RunnerIntegrationGrant{}
			definition := r.integrationDefinitions["context7"]
			for _, grant := range c.Grants {
				capability, ok := definition.Capability(grant.CapabilityKey)
				if !ok {
					t.Fatal("capability missing")
				}
				schema, _ := capability.InputSchema()
				schemaDigest, _ := capability.InputSchemaDigest()
				grants = append(grants, runtimecontract.RunnerIntegrationGrant{Ref: grant.Ref, GrantVersion: grant.Version, ConnectionRef: c.Ref, ConnectionVersion: c.Version, ApprovalPolicy: grant.ApprovalPolicy, DefinitionKey: c.DefinitionKey, DefinitionVersion: c.DefinitionVersion, DefinitionDigest: c.DefinitionDigest, ConnectionName: c.Name, CapabilityKey: grant.CapabilityKey, CapabilityName: capability.Name, CapabilityDescription: capability.Description, Risk: capability.Risk, Operation: capability.Operation, InputSchema: string(schema), InputSchemaSHA256: schemaDigest})
			}
			tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var organizationID string
			if err := tx.QueryRow(ctx, `SELECT organization_id::text FROM control_plane.integration_connections WHERE ref=$1`, c.Ref).Scan(&organizationID); err != nil {
				t.Fatal(err)
			}
			return runtimeManagedMCPProfiles(ctx, tx, organizationID, "AGENT", agent.Ref, agent.Ref, project.Ref, grants)
		}
		profiles, err := readProof(c)
		if err != nil || len(profiles) != 1 || profiles[0].Health.TestRef != stringMap(item, "testRef") || profiles[0].Health.ConnectionVersion != c.Version {
			t.Fatal("grant-only version change lost exact semantic health receipt")
		}
		// Настоящая новая grant version, без изменения configuration/credential.
		c = *execute(command.ChangeIntegrationGrant, "grant-update", &c.Version, command.IntegrationGrantInput{ConnectionRef: c.Ref, CapabilityKey: runtimecontract.Context7ResolveCapability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
		updated, err := readProof(c)
		if err != nil || len(updated) != 1 || updated[0].Digest == profiles[0].Digest {
			t.Fatal("fresh profile did not bind new grant version")
		}
		mutateConfig(c.Ref)
		c.Version++
		if _, err := readProof(c); err == nil {
			t.Fatal("semantic configuration drift retained health authority")
		}
	})
	t.Run("old terminal cannot authorize required profile", func(t *testing.T) {
		c := connection("legacy")
		// Даже synthetic terminal с совпадающими current inputs не становится
		// proof без реальной claimed generation. История до migration имела 0.
		_, err := pool.Exec(ctx, `INSERT INTO control_plane.integration_connection_tests
		(ref,organization_id,connection_id,state,generation,claimed_workload,completed_at,created_by)
		SELECT 'tst_legacy001',organization_id,id,'SUCCEEDED',0,'integration-gateway',clock_timestamp(),created_by
		FROM control_plane.integration_connections WHERE ref=$1`, c.Ref)
		if err != nil {
			t.Fatal("prepare isolated legacy terminal")
		}
		definition := r.integrationDefinitions["context7"]
		grants := []runtimecontract.RunnerIntegrationGrant{}
		for index, key := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
			capability, ok := definition.Capability(key)
			if !ok {
				t.Fatal("legacy capability missing")
			}
			schema, _ := capability.InputSchema()
			digest, _ := capability.InputSchemaDigest()
			grants = append(grants, runtimecontract.RunnerIntegrationGrant{Ref: []string{"igr_legacy001", "igr_legacy002"}[index], GrantVersion: 1,
				ConnectionRef: c.Ref, ConnectionVersion: c.Version, ApprovalPolicy: "NONE", DefinitionKey: c.DefinitionKey, DefinitionVersion: c.DefinitionVersion, DefinitionDigest: c.DefinitionDigest,
				ConnectionName: c.Name, CapabilityKey: key, CapabilityName: capability.Name, CapabilityDescription: capability.Description, Risk: "READ", Operation: capability.Operation, InputSchema: string(schema), InputSchemaSHA256: digest})
		}
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var organizationID string
		if err := tx.QueryRow(ctx, `SELECT organization_id::text FROM control_plane.integration_connections WHERE ref=$1`, c.Ref).Scan(&organizationID); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeManagedMCPProfiles(ctx, tx, organizationID, "AGENT", "agt_legacy001", "agt_legacy001", "prj_legacy001", grants); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("historical terminal granted health authority")
		}
	})
}
