package platform

import (
	"bytes"
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
	connectionWithDefinition := func(key, definition string) entity.IntegrationConnection {
		t.Helper()
		configuration := map[string]any{"base_url": "https://mcp.context7.com"}
		if definition == "github" {
			configuration = map[string]any{"owner": "acme", "repository": "fixture"}
		}
		created := execute(command.CreateConnection, key+"-create", nil, command.ConnectionInput{DefinitionKey: definition, Name: "Synthetic health " + key, PublicConfiguration: configuration}).Connection
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
	connection := func(key string) entity.IntegrationConnection { return connectionWithDefinition(key, "context7") }
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

	t.Run("projection pending READ retry preserves exact snapshot and bounded fenced attempts", func(t *testing.T) {
		for _, definition := range []string{"github", "context7"} {
			t.Run(definition, func(t *testing.T) {
				c, ref := start("projection-"+definition, connectionWithDefinition("projection-"+definition, definition))
				first := claim(c.Ref)
				if first == nil || first["credential"] == nil {
					t.Fatal("exact credential health claim missing")
				}
				var before, after []byte
				if err := pool.QueryRow(ctx, `SELECT input_snapshot FROM control_plane.integration_connection_tests WHERE ref=$1`, ref).Scan(&before); err != nil {
					t.Fatal(err)
				}
				var writesBefore, writesAfter int
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.integration_invocations`).Scan(&writesBefore); err != nil {
					t.Fatal(err)
				}
				pending := payload(first)
				pending.Success, pending.SafeErrorCode = false, credentialProjectionPendingCode
				result, err := complete("projection-"+definition+"-pending", first, pending)
				if err != nil || result.Connection == nil || result.Connection.State != "TESTING" || result.Connection.MaskedCredentialsState != "CONFIGURED" {
					t.Fatalf("projection pending invalidated credential: %v", err)
				}
				if claim(c.Ref) != nil {
					t.Fatal("projection retry skipped backoff")
				}
				if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET updated_at=clock_timestamp()-INTERVAL '6 seconds' WHERE ref=$1`, ref); err != nil {
					t.Fatal(err)
				}
				second := claim(c.Ref)
				if second == nil || stringMap(second, "testRef") != ref || second["generation"].(int64) <= first["generation"].(int64) || stringMap(second, "fence") == stringMap(first, "fence") || stringMap(second, "leaseRef") == stringMap(first, "leaseRef") {
					t.Fatal("projection retry reused fenced claim")
				}
				if err := pool.QueryRow(ctx, `SELECT input_snapshot FROM control_plane.integration_connection_tests WHERE ref=$1`, ref).Scan(&after); err != nil || !bytes.Equal(before, after) {
					t.Fatal("projection retry replaced immutable input")
				}
				if _, err := complete("projection-"+definition+"-stale", first, payload(first)); !errors.Is(err, errs.ErrForbidden) {
					t.Fatalf("old lease completed new attempt: %v", err)
				}
				if result, err := complete("projection-"+definition+"-ready", second, payload(second)); err != nil || result.Connection == nil || result.Connection.State != "CONNECTED" {
					t.Fatalf("fresh projection test did not complete: %v", err)
				}
				if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.integration_invocations`).Scan(&writesAfter); err != nil || writesAfter != writesBefore {
					t.Fatal("READ projection retry created invocation/WRITE work")
				}
			})
		}
	})
	t.Run("projection pending does not retry permanent or general provider failures", func(t *testing.T) {
		for index, code := range []string{"INTEGRATION_AUTH_REJECTED", "INTEGRATION_CREDENTIAL_UNAVAILABLE", "INTEGRATION_UNAVAILABLE", "INTEGRATION_RESPONSE_INVALID"} {
			key := "projection-permanent-" + string(rune('a'+index))
			c, _ := start(key, connectionWithDefinition(key, "github"))
			item := claim(c.Ref)
			if item == nil {
				t.Fatal("permanent health claim missing")
			}
			bad := payload(item)
			bad.Success, bad.SafeErrorCode = false, code
			result, err := complete(key, item, bad)
			if err != nil || result.Connection == nil || result.Connection.State != "DEGRADED" || claim(c.Ref) != nil {
				t.Fatalf("permanent/general failure was retried: %s %v", code, err)
			}
		}
	})
	t.Run("projection pending stops at attempt or time budget", func(t *testing.T) {
		for _, limit := range []string{"attempt", "time"} {
			key := "projection-exhausted-" + limit
			c, ref := start(key, connectionWithDefinition(key, "github"))
			item := claim(c.Ref)
			if item == nil {
				t.Fatal("budget health claim missing")
			}
			query := `UPDATE control_plane.integration_connection_tests SET attempt=8 WHERE ref=$1`
			if limit == "time" {
				query = `UPDATE control_plane.integration_connection_tests SET created_at=clock_timestamp()-INTERVAL '91 seconds' WHERE ref=$1`
			}
			if _, err := pool.Exec(ctx, query, ref); err != nil {
				t.Fatal(err)
			}
			bad := payload(item)
			bad.Success, bad.SafeErrorCode = false, credentialProjectionPendingCode
			result, err := complete(key, item, bad)
			if err != nil || result.Connection == nil || result.Connection.State != "DEGRADED" || result.Connection.MaskedCredentialsState != "CONFIGURED" || claim(c.Ref) != nil {
				t.Fatalf("projection budget retried or invalidated credential: %s %v", limit, err)
			}
		}
	})
	t.Run("projection pending does not adopt current configuration or credential drift", func(t *testing.T) {
		for _, drift := range []string{"configuration", "credential"} {
			key := "projection-drift-" + drift
			c, _ := start(key, connectionWithDefinition(key, "github"))
			item := claim(c.Ref)
			if item == nil {
				t.Fatal("drift health claim missing")
			}
			if drift == "configuration" {
				mutateConfig(c.Ref)
			} else if _, err := pool.Exec(ctx, `WITH credential AS (
			INSERT INTO control_plane.integration_credential_revisions(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
			SELECT 'icr_drift_'||ref,organization_id,id,2,'kodex-system/synthetic#new_key','60000000-0000-4000-8000-000000000001'::uuid,'2',repeat('b',64),created_by
			FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
			UPDATE control_plane.integration_connections c SET credential_revision_id=credential.id,version=version+1 FROM credential WHERE c.id=credential.connection_id`, c.Ref); err != nil {
				t.Fatal(err)
			}
			bad := payload(item)
			bad.Success, bad.SafeErrorCode = false, credentialProjectionPendingCode
			if _, err := complete(key, item, bad); err == nil || claim(c.Ref) != nil {
				t.Fatal("changed current pins authorized retry")
			}
		}
	})
	t.Run("projection pending cannot revive revoked owner test", func(t *testing.T) {
		c, _ := start("projection-revoked", connectionWithDefinition("projection-revoked", "github"))
		item := claim(c.Ref)
		if item == nil {
			t.Fatal("revoke health claim missing")
		}
		execute(command.SetConnectionEnabled, "projection-disable", &c.Version, command.ConnectionInput{Ref: c.Ref, Enabled: false})
		bad := payload(item)
		bad.Success, bad.SafeErrorCode = false, credentialProjectionPendingCode
		if _, err := complete("projection-revoked", item, bad); err == nil || claim(c.Ref) != nil {
			t.Fatal("revoked test was requeued")
		}
	})
	t.Run("projection pending is forbidden for invocation WRITE completion", func(t *testing.T) {
		invocationWorker := principal("integration-gateway", "platform.runtime.integrations.claim", "kodex-system-subject", "kodex-installation")
		input := command.Command{Kind: command.CompleteIntegrationInvocation, Principal: invocationWorker,
			Mutation: value.Mutation{IdempotencyKey: "health-projection-write-rejected"},
			Payload: command.IntegrationInvocationInput{InvocationRef: "inv_projected_write", LeaseRef: "lea_projected_write", Fence: "fnc_projected_write",
				Generation: 1, Success: false, SafeErrorCode: credentialProjectionPendingCode}}
		if _, err := service.Execute(ctx, input); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("unowned invocation completion accepted: %v", err)
		}
		// Даже до разрешения реального invocation специализированный adapter
		// отклоняет test-only код: наличие WRITE claim не изменит этот allowlist.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := r.completeIntegrationInvocation(ctx, tx, scope{}, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("test-only code authorized invocation completion: %v", err)
		}
	})

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
	t.Run("background refresh ledger failure recovery expiry and revoke", func(t *testing.T) {
		c := connection("refresh")
		project := execute(command.CreateProject, "refresh-project", nil, command.ProjectInput{Name: "Refresh fixture", Language: "en"}).Project
		agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "refresh-agent", "Refresh reader")
		for _, key := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
			c = *execute(command.ChangeIntegrationGrant, "refresh-"+key, &c.Version, command.IntegrationGrantInput{
				ConnectionRef: c.Ref, CapabilityKey: key, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
		}
		// Только synthetic real-probe ledger metadata; сетевого adapter нет.
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.integration_connection_tests
			(ref,organization_id,connection_id,state,generation,claimed_workload,completed_at,created_by)
			SELECT 'tst_refreshold',organization_id,id,'SUCCEEDED',1,'integration-gateway',
			clock_timestamp()-INTERVAL '6 minutes',created_by FROM control_plane.integration_connections WHERE ref=$1`, c.Ref); err != nil {
			t.Fatal("prepare expired synthetic receipt")
		}
		var organizationID string
		if err := pool.QueryRow(ctx, `SELECT organization_id::text FROM control_plane.integration_connections WHERE ref=$1`, c.Ref).Scan(&organizationID); err != nil {
			t.Fatal("read synthetic health organization")
		}
		grants := func(c entity.IntegrationConnection) []runtimecontract.RunnerIntegrationGrant {
			result := []runtimecontract.RunnerIntegrationGrant{}
			for _, g := range c.Grants {
				capability, _ := r.integrationDefinitions["context7"].Capability(g.CapabilityKey)
				schema, _ := capability.InputSchema()
				digest, _ := capability.InputSchemaDigest()
				result = append(result, runtimecontract.RunnerIntegrationGrant{Ref: g.Ref, GrantVersion: g.Version,
					ConnectionRef: c.Ref, ConnectionVersion: c.Version, ApprovalPolicy: g.ApprovalPolicy, DefinitionKey: c.DefinitionKey,
					DefinitionVersion: c.DefinitionVersion, DefinitionDigest: c.DefinitionDigest, ConnectionName: c.Name,
					CapabilityKey: g.CapabilityKey, CapabilityName: capability.Name, CapabilityDescription: capability.Description,
					Risk: capability.Risk, Operation: capability.Operation, InputSchema: string(schema), InputSchemaSHA256: digest})
			}
			return result
		}
		proof := func(c entity.IntegrationConnection) error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("open synthetic proof transaction")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = runtimeManagedMCPProfiles(ctx, tx, organizationID, "AGENT", agent.Ref, agent.Ref, project.Ref, grants(c))
			return err
		}
		if !errors.Is(proof(c), errs.ErrConflict) {
			t.Fatal("expired receipt authorized required MCP")
		}
		version := c.Version
		var eventBefore, eventAfter int64
		item := claim(c.Ref)
		if item == nil {
			t.Fatal("owner did not enqueue and claim required refresh")
		}
		if duplicate := claim(c.Ref); duplicate != nil {
			t.Fatal("duplicate concurrent refresh was claimed")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET purpose='OWNER_TEST' WHERE ref=$1`, stringMap(item, "testRef")); err == nil {
			t.Fatal("worker task origin changed")
		}
		run := execute(command.LaunchRun, "refresh-run", nil, command.LaunchRunInput{ProjectRef: project.Ref,
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Synthetic MCP owner health read"}).Run
		controller := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
		runtimeClaim := func(key string) command.Result {
			t.Helper()
			result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: controller,
				Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: "health-runtime-fixture", Limit: 32}})
			if err != nil {
				t.Fatal("synthetic readiness claim failed")
			}
			return result
		}
		assertPending := func() {
			t.Helper()
			readback, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil || readback.State != run.State {
				t.Fatal("pending probe terminalized or started the runtime graph")
			}
			var revisions, leases int
			if err := pool.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM control_plane.runtime_revisions revision JOIN control_plane.run_nodes n ON n.id=revision.node_id JOIN control_plane.runs r ON r.id=n.run_id WHERE r.ref=$1),
				(SELECT count(*) FROM control_plane.runtime_leases lease JOIN control_plane.run_nodes n ON n.id=lease.node_id JOIN control_plane.runs r ON r.id=n.run_id WHERE r.ref=$1)`, run.Ref).Scan(&revisions, &leases); err != nil || revisions != 0 || leases != 0 {
				t.Fatal("pending probe published RuntimeRevision or execution grant")
			}
		}
		if len(runtimeClaim("health-pending-first").RuntimeItems) != 0 {
			t.Fatal("pending probe was treated as successful health")
		}
		assertPending()
		// Новая owner transaction/instance не сбрасывает durable wait window.
		if len(runtimeClaim("health-pending-restart").RuntimeItems) != 0 {
			t.Fatal("new poll bypassed pending readiness")
		}
		assertPending()
		healthy := createLifecycleAgent(t, ctx, service, owner, project.Ref, "refresh-healthy", "Independent reader")
		healthyRun := execute(command.LaunchRun, "refresh-healthy-run", nil, command.LaunchRunInput{ProjectRef: project.Ref,
			Target: entity.RunTarget{Type: "AGENT", Ref: healthy.Ref}, Task: "Synthetic independent work"}).Run
		mixed := runtimeClaim("health-pending-mixed")
		if len(mixed.RuntimeItems) != 1 || stringMap(mixed.RuntimeItems[0], "runRef") != healthyRun.Ref {
			t.Fatal("pending candidate blocked unrelated eligible work")
		}
		assertPending()
		if err := pool.QueryRow(ctx, `SELECT platform_sequence FROM control_plane.installation`).Scan(&eventBefore); err != nil {
			t.Fatal("read initial event sequence")
		}
		finished, err := complete("refresh-ok", item, payload(item))
		if err != nil || finished.Connection == nil || finished.Connection.Version != version || finished.Connection.State != "CONNECTED" {
			t.Fatal("successful refresh churned connection pins")
		}
		c = *finished.Connection
		if proof(c) != nil {
			t.Fatal("actual claimed refresh receipt did not restore freshness")
		}
		if err := pool.QueryRow(ctx, `SELECT platform_sequence FROM control_plane.installation`).Scan(&eventAfter); err != nil || eventBefore != eventAfter {
			t.Fatal("successful health-only refresh emitted configuration event")
		}
		claimed := runtimeClaim("health-refresh-runtime-claim")
		if len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != run.Ref {
			t.Fatal("fresh health did not permit immutable runtime claim")
		}
		var nodeID string
		if err := pool.QueryRow(ctx, `SELECT n.id::text FROM control_plane.run_nodes n JOIN control_plane.runs r ON r.id=n.run_id WHERE r.ref=$1 AND n.ref=$2`, run.Ref, stringMap(claimed.RuntimeItems[0], "nodeRef")).Scan(&nodeID); err != nil {
			t.Fatal("read synthetic claimed node")
		}
		currentHealth := func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin owner health read")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			return requireCurrentManagedMCPHealth(ctx, tx, organizationID, nodeID, c.Ref)
		}
		if healthErr := currentHealth(); healthErr != nil {
			var raw []byte
			if err := pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE node_id=$1::uuid`, nodeID).Scan(&raw); err != nil {
				t.Fatal("read synthetic execution projection")
			}
			var snapshot map[string]json.RawMessage
			_ = json.Unmarshal(raw, &snapshot)
			var profiles []runtimecontract.ManagedMCPProfile
			_ = json.Unmarshal(snapshot["managedMCPProfiles"], &profiles)
			_, decodeErr := managedMCPInputFromOwnerSnapshot(raw)
			t.Fatalf("protected owner health rejected synthetic execution: class=%s profiles=%d decode_invalid=%t", runtimeEligibilityErrorClass(healthErr), len(profiles), decodeErr != nil)
		}
		createDue := func(ref string) {
			t.Helper()
			if _, err := pool.Exec(ctx, `INSERT INTO control_plane.integration_connection_tests
				(ref,organization_id,connection_id,state,created_by,purpose)
				SELECT $2,organization_id,id,'DUE',created_by,'MANAGED_MCP_REFRESH'
				FROM control_plane.integration_connections WHERE ref=$1`, c.Ref, ref); err != nil {
				t.Fatal("create synthetic bounded refresh")
			}
		}
		var immutableBefore, immutableAfter []byte
		if err := pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE node_id=$1::uuid`, nodeID).Scan(&immutableBefore); err != nil {
			t.Fatal("read initial immutable MCP input")
		}
		createDue("tst_refreshsecond")
		second := claim(c.Ref)
		if second == nil {
			t.Fatal("second real probe was not claimed")
		}
		secondResult, err := complete("refresh-second", second, payload(second))
		if err != nil || secondResult.Connection.Version != c.Version || currentHealth() != nil {
			t.Fatal("new fresh observation changed immutable call eligibility")
		}
		if err := pool.QueryRow(ctx, `SELECT safe_snapshot FROM control_plane.runtime_revisions WHERE node_id=$1::uuid`, nodeID).Scan(&immutableAfter); err != nil || !bytes.Equal(immutableBefore, immutableAfter) {
			t.Fatal("background probe rewrote immutable execution")
		}
		// Все success receipts этих synthetic inputs имеют ту же current version.
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET completed_at=clock_timestamp()-INTERVAL '6 minutes' WHERE connection_id=(SELECT id FROM control_plane.integration_connections WHERE ref=$1) AND state='SUCCEEDED'`, c.Ref); err != nil {
			t.Fatal("expire isolated same-input health receipts")
		}
		if currentHealth() == nil {
			t.Fatal("live call bypassed current owner health TTL")
		}
		renewal := claim(c.Ref)
		if renewal == nil {
			t.Fatal("expired owner health was not refreshed automatically")
		}
		if _, err := complete("refresh-renewed", renewal, payload(renewal)); err != nil || currentHealth() != nil {
			t.Fatal("fresh owner probe did not restore unchanged immutable execution")
		}
		createDue("tst_refreshfail")
		failed := claim(c.Ref)
		if failed == nil {
			t.Fatal("failure fixture was not claimed")
		}
		bad := payload(failed)
		bad.Success, bad.SafeErrorCode = false, "INTEGRATION_UNAVAILABLE"
		failure, err := complete("refresh-failed", failed, bad)
		if err != nil || failure.Connection.State != "DEGRADED" || failure.Connection.Version != c.Version+1 {
			t.Fatal("failed refresh did not close readiness")
		}
		c = *failure.Connection
		if proof(c) == nil {
			t.Fatal("prior fresh receipt survived failed refresh")
		}
		if currentHealth() == nil {
			t.Fatal("live invocation retained health authority after probe failure")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET retry_after=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(failed, "testRef")); err != nil {
			t.Fatal("advance isolated retry clock")
		}
		recovery := claim(c.Ref)
		if recovery == nil || stringMap(recovery, "testRef") == stringMap(failed, "testRef") {
			t.Fatal("recovery did not create a new immutable task")
		}
		recovered, err := complete("refresh-recovered", recovery, payload(recovery))
		if err != nil || recovered.Connection.State != "CONNECTED" || recovered.Connection.Version != c.Version+1 || proof(*recovered.Connection) != nil {
			t.Fatal("own same-input health recovery failed")
		}
		c = *recovered.Connection
		createDue("tst_refreshexpiry")
		expired := claim(c.Ref)
		if expired == nil {
			t.Fatal("expiry fixture was not claimed")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET lease_expires_at=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(expired, "testRef")); err != nil {
			t.Fatal("expire isolated refresh lease")
		}
		if claim(c.Ref) != nil {
			t.Fatal("expired attempt bypassed retry backoff")
		}
		if _, err := complete("refresh-expired-completion", expired, payload(expired)); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("expired worker published health authority")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET retry_after=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(expired, "testRef")); err != nil {
			t.Fatal("advance isolated expiry backoff")
		}
		retry := claim(c.Ref)
		if retry == nil || stringMap(retry, "testRef") == stringMap(expired, "testRef") {
			t.Fatal("expired retry reused prior receipt")
		}
		c = *execute(command.ChangeIntegrationGrant, "refresh-revoke", &c.Version, command.IntegrationGrantInput{
			ConnectionRef: c.Ref, CapabilityKey: runtimecontract.Context7QueryCapability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: false}).Connection
		if claim(c.Ref) != nil {
			t.Fatal("revoked pair continued refresh")
		}
		if _, err := complete("refresh-revoked-completion", retry, payload(retry)); err == nil {
			t.Fatal("revoked refresh published a successful receipt")
		}
		if currentHealth() == nil {
			t.Fatal("revoked grant retained immutable invocation authority")
		}
		preparePair := func(key string) entity.IntegrationConnection {
			t.Helper()
			item := connection(key)
			for _, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
				item = *execute(command.ChangeIntegrationGrant, key+capability, &item.Version, command.IntegrationGrantInput{
					ConnectionRef: item.Ref, CapabilityKey: capability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
			}
			return item
		}
		limitConnection := preparePair("retrylimit")
		for attempt := 1; attempt <= 3; attempt++ {
			work := claim(limitConnection.Ref)
			if work == nil {
				t.Fatal("bounded retry fixture was not claimed")
			}
			var actualAttempt int
			if err := pool.QueryRow(ctx, `SELECT attempt FROM control_plane.integration_connection_tests WHERE ref=$1`, stringMap(work, "testRef")).Scan(&actualAttempt); err != nil || actualAttempt != attempt {
				t.Fatal("retry task lost attempt lineage")
			}
			failure := payload(work)
			failure.Success, failure.SafeErrorCode = false, "INTEGRATION_UNAVAILABLE"
			if _, err := complete("retry-limit-"+string(rune('0'+attempt)), work, failure); err != nil {
				t.Fatal("complete bounded retry failure")
			}
			if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET retry_after=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(work, "testRef")); err != nil {
				t.Fatal("advance bounded retry clock")
			}
		}
		if claim(limitConnection.Ref) != nil {
			t.Fatal("exhausted health retry created unbounded work")
		}
		driftConnection := preparePair("recoverdrift")
		driftWork := claim(driftConnection.Ref)
		if driftWork == nil {
			t.Fatal("drift fixture was not claimed")
		}
		driftFailure := payload(driftWork)
		driftFailure.Success, driftFailure.SafeErrorCode = false, "INTEGRATION_UNAVAILABLE"
		if _, err := complete("drift-failure", driftWork, driftFailure); err != nil {
			t.Fatal("complete drift fixture failure")
		}
		mutateConfig(driftConnection.Ref)
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET retry_after=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(driftWork, "testRef")); err != nil {
			t.Fatal("advance drift retry clock")
		}
		if claim(driftConnection.Ref) != nil {
			t.Fatal("background recovery adopted configuration drift")
		}
		if _, err := pool.Exec(ctx, `INSERT INTO control_plane.integration_connection_tests
			(ref,organization_id,connection_id,state,created_by,purpose,attempt,predecessor_ref)
			SELECT 'tst_foreignprevious',organization_id,id,'DUE',created_by,'MANAGED_MCP_REFRESH',2,$2
			FROM control_plane.integration_connections WHERE ref=$1`, limitConnection.Ref, stringMap(driftWork, "testRef")); err == nil {
			t.Fatal("cross-connection predecessor authorized health retry")
		}
	})

	t.Run("startup pending is exact and durably bounded", func(t *testing.T) {
		project := execute(command.CreateProject, "pending-project", nil, command.ProjectInput{Name: "Pending fixture", Language: "en"}).Project
		type fixture struct {
			connection     entity.IntegrationConnection
			agent          entity.Agent
			organizationID string
			grants         []runtimecontract.RunnerIntegrationGrant
		}
		prepare := func(key string) fixture {
			t.Helper()
			c := connection("pending-" + key)
			a := createLifecycleAgent(t, ctx, service, owner, project.Ref, "pending-agent-"+key, "Pending reader "+key)
			for _, key := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
				c = *execute(command.ChangeIntegrationGrant, "pending-"+a.Ref+key, &c.Version, command.IntegrationGrantInput{
					ConnectionRef: c.Ref, CapabilityKey: key, AgentRef: a.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
			}
			f := fixture{connection: c, agent: a}
			if err := pool.QueryRow(ctx, `SELECT organization_id::text FROM control_plane.integration_connections WHERE ref=$1`, c.Ref).Scan(&f.organizationID); err != nil {
				t.Fatal("read pending fixture organization")
			}
			for _, g := range c.Grants {
				capability, _ := r.integrationDefinitions["context7"].Capability(g.CapabilityKey)
				schema, _ := capability.InputSchema()
				digest, _ := capability.InputSchemaDigest()
				f.grants = append(f.grants, runtimecontract.RunnerIntegrationGrant{Ref: g.Ref, GrantVersion: g.Version, ConnectionRef: c.Ref, ConnectionVersion: c.Version,
					ApprovalPolicy: g.ApprovalPolicy, DefinitionKey: c.DefinitionKey, DefinitionVersion: c.DefinitionVersion, DefinitionDigest: c.DefinitionDigest,
					ConnectionName: c.Name, CapabilityKey: g.CapabilityKey, CapabilityName: capability.Name, CapabilityDescription: capability.Description,
					Risk: capability.Risk, Operation: capability.Operation, InputSchema: string(schema), InputSchemaSHA256: digest})
			}
			return f
		}
		startup := func(f fixture, capabilityScopes ...[]string) error {
			t.Helper()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin pending fixture read")
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := requireManagedMCPStartupDependencies(ctx, tx, f.organizationID, f.agent.Ref, f.grants, capabilityScopes...); err != nil {
				return err
			}
			_, err = runtimeManagedMCPProfilesForStartup(ctx, tx, f.organizationID, "AGENT", f.agent.Ref, f.agent.Ref, project.Ref, f.grants)
			return err
		}
		due := func(f fixture, key string, ageSeconds int, attempt int, predecessor string) {
			t.Helper()
			if _, err := pool.Exec(ctx, `INSERT INTO control_plane.integration_connection_tests
				(ref,organization_id,connection_id,state,created_by,purpose,created_at,attempt,predecessor_ref)
				SELECT $2,organization_id,id,'DUE',created_by,'MANAGED_MCP_REFRESH',clock_timestamp()-$3*INTERVAL '1 second',$4,NULLIF($5,'')
				FROM control_plane.integration_connections WHERE ref=$1`, f.connection.Ref, key, ageSeconds, attempt, predecessor); err != nil {
				t.Fatal("insert bounded pending fixture")
			}
		}
		cold := prepare("cold")
		if !errors.Is(startup(cold), errs.ErrConflict) {
			t.Fatal("missing real probe did not fail closed")
		}
		excluded := cold
		excluded.grants = nil
		for _, scopes := range [][][]string{{{"platform.run.delegate"}, nil}, {{}, nil}, {nil, {}}} {
			if err := startup(excluded, scopes...); err != nil {
				t.Fatal("excluded Context7 dependency blocked constrained execution")
			}
		}
		for _, capability := range []string{runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability} {
			if !errors.Is(startup(excluded, []string{capability}), errs.ErrConflict) {
				t.Fatal("allowed Context7 capability did not require full dependency pair")
			}
		}
		if !errors.Is(startup(cold, []string{"platform.run.delegate"}), errs.ErrConflict) {
			t.Fatal("excluded scope adopted Context7 grants")
		}
		due(cold, "tst_pendingcold", 0, 1, "")
		for poll := 0; poll < 2; poll++ {
			if !errors.Is(startup(cold), errManagedMCPHealthPending) {
				t.Fatal("exact DUE probe was not pending across transactions")
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET created_at=clock_timestamp() WHERE ref='tst_pendingcold'`); err == nil {
			t.Fatal("restart reset immutable pending budget")
		}
		timed := prepare("timeout")
		due(timed, "tst_pendingtimeout", 31, 1, "")
		if !errors.Is(startup(timed), errs.ErrConflict) {
			t.Fatal("expired 30-second pending window did not reject")
		}
		// Новая attempt не получает новый startup wait budget старого цикла.
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET state='FAILED',completed_at=clock_timestamp(),retry_after=clock_timestamp() WHERE ref='tst_pendingtimeout'`); err != nil {
			t.Fatal("finish synthetic timed out probe")
		}
		due(timed, "tst_pendingretry", 0, 2, "tst_pendingtimeout")
		if !errors.Is(startup(timed), errs.ErrConflict) {
			t.Fatal("retry reset durable first-attempt window")
		}
		changed := prepare("config")
		due(changed, "tst_pendingconfig", 0, 1, "")
		mutateConfig(changed.connection.Ref)
		if !errors.Is(startup(changed), errs.ErrConflict) {
			t.Fatal("configuration drift was hidden by pending probe")
		}
		revoked := prepare("revoke")
		due(revoked, "tst_pendingrevoke", 0, 1, "")
		_ = execute(command.ChangeIntegrationGrant, "pending-revoke", &revoked.connection.Version, command.IntegrationGrantInput{
			ConnectionRef: revoked.connection.Ref, CapabilityKey: runtimecontract.Context7QueryCapability, AgentRef: revoked.agent.Ref, ApprovalPolicy: "NONE", Enabled: false})
		if !errors.Is(startup(revoked), errs.ErrConflict) {
			t.Fatal("revoked grant was hidden by pending probe")
		}
		expired := prepare("lease")
		work := claim(expired.connection.Ref)
		if work == nil {
			t.Fatal("claim exact lease fixture")
		}
		if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connection_tests SET lease_expires_at=clock_timestamp()-INTERVAL '1 second' WHERE ref=$1`, stringMap(work, "testRef")); err != nil {
			t.Fatal("expire pending fixture lease")
		}
		if !errors.Is(startup(expired), errs.ErrConflict) {
			t.Fatal("expired lease was treated as current pending proof")
		}
		failure := prepare("failed")
		work = claim(failure.connection.Ref)
		if work == nil {
			t.Fatal("claim failed refresh fixture")
		}
		bad := payload(work)
		bad.Success, bad.SafeErrorCode = false, "INTEGRATION_UNAVAILABLE"
		if _, err := complete("pending-failed", work, bad); err != nil {
			t.Fatal("finish failed pending fixture")
		}
		// Общий callable path исключит DEGRADED grants; required dependency всё
		// равно должен остановить startup, не превратить его в пустой MCP profile.
		failure.grants = nil
		if !errors.Is(startup(failure), errs.ErrConflict) {
			t.Fatal("failed refresh silently removed required MCP")
		}
		controller := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
		run := execute(command.LaunchRun, "pending-failed-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Target: entity.RunTarget{Type: "AGENT", Ref: failure.agent.Ref}, Task: "Synthetic failed dependency"}).Run
		if result, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: controller, Mutation: value.Mutation{IdempotencyKey: "pending-failed-runtime"}, Payload: command.LeaseInput{WorkloadInstance: "pending-failed-worker", Limit: 32}}); err != nil || len(result.RuntimeItems) != 0 {
			t.Fatal("failed dependency created runtime work")
		}
		if result, err := service.GetRun(ctx, owner, run.Ref); err != nil || result.State != "FAILED" {
			t.Fatal("failed dependency did not close runtime graph")
		}
	})
}
