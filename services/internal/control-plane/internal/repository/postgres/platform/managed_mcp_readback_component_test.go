package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/managed_mcp_readback_fixture.sql
var queryManagedMCPReadbackFixture string

//go:embed testdata/sql/managed_mcp_readback_stale.sql
var queryManagedMCPReadbackStale string

//go:embed testdata/sql/managed_mcp_readback_recredential.sql
var queryManagedMCPReadbackRecredential string

// Выполняет поставляемую read-only SQL на отдельной мигрированной БД. Health
// completion синтетический; внешний adapter, provider и live данные не нужны.
func TestManagedMCPHealthReadbackComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated readback PostgreSQL")
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
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	gateway := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-tests.claim"}, "integration-gateway")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "readback-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("readback synthetic command %s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Readback fixture", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "readback-agent", "Architect")
	connection := *execute(command.CreateConnection, "connection", nil, command.ConnectionInput{DefinitionKey: "context7", Name: "Readback Context7", PublicConfiguration: map[string]any{"base_url": "https://mcp.context7.com"}}).Connection
	if _, err = pool.Exec(ctx, queryManagedMCPReadbackFixture, pgx.StrictNamedArgs{"connection_ref": connection.Ref}); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{"context7.library.resolve", "context7.docs.query"} {
		connection = *execute(command.ChangeIntegrationGrant, capability, &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: capability, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true}).Connection
	}
	var resolveRef, queryRef string
	for _, grant := range connection.Grants {
		if grant.CapabilityKey == "context7.library.resolve" {
			resolveRef = grant.Ref
		}
		if grant.CapabilityKey == "context7.docs.query" {
			queryRef = grant.Ref
		}
	}
	source, err := os.ReadFile("../../../../../../../tools/release/managed-mcp-health-readback.sql")
	if err != nil {
		t.Fatal("read shipped diagnostic SQL")
	}
	readback := func(agentRef string, version int64) map[string]any {
		t.Helper()
		params := map[string]string{"connection_ref": connection.Ref, "agent_ref": agentRef, "connection_version": strconv.FormatInt(version, 10), "resolve_ref": resolveRef, "resolve_version": "1", "query_ref": queryRef, "query_version": "1"}
		query := string(source)
		for key, value := range params {
			query = strings.ReplaceAll(query, ":'"+key+"'", "'"+strings.ReplaceAll(value, "'", "''")+"'")
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		results, err := conn.Conn().PgConn().Exec(ctx, query).ReadAll()
		if err != nil {
			t.Fatal("read-only diagnostic query failed", err)
		}
		for _, result := range results {
			if result.Err != nil {
				t.Fatal("read-only diagnostic statement failed", result.Err)
			}
			if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
				var value map[string]any
				if json.Unmarshal(result.Rows[0][0], &value) == nil && value["observedAt"] != nil {
					input, err := json.Marshal(map[string]any{"connectionRef": connection.Ref, "agentRef": agentRef, "connectionVersion": version, "resolveRef": resolveRef, "resolveVersion": 1, "queryRef": queryRef, "queryVersion": 1})
					if err != nil {
						t.Fatal("encode safe diagnostic input")
					}
					module, err := filepath.Abs("../../../../../../../tools/release/managed-mcp-health-readback.mjs")
					if err != nil {
						t.Fatal("resolve diagnostic module")
					}
					checker := exec.CommandContext(ctx, "node", "--input-type=module", "--eval", `
import { pathToFileURL } from 'node:url';
const { diagnoseManagedMCPHealth } = await import(pathToFileURL(process.argv[2]));
let raw = ''; for await (const chunk of process.stdin) raw += chunk;
try {
  const result = diagnoseManagedMCPHealth(JSON.parse(process.argv[3]), JSON.parse(raw));
  process.stdout.write(result.status);
} catch (error) {
  process.stdout.write(/^READBACK_[A-Z_]+$/.test(error.message) ? error.message : 'READBACK_FAILED');
  process.exitCode = 1;
}
`, "fixture-classifier", module, string(input))
					checker.Env = []string{"PATH=" + os.Getenv("PATH")}
					checker.Stdin = strings.NewReader(string(result.Rows[0][0]))
					if output, err := checker.Output(); err != nil {
						t.Fatal("shipped classifier rejected SQL projection", string(output), err)
					}
					return value
				}
			}
		}
		t.Fatal("safe readback result missing")
		return nil
	}
	current := readback(agent.Ref, connection.Version)
	if current["scopeFound"] != true || current["requiredGrantCount"] != float64(2) || current["matchedGrantCount"] != float64(2) || current["freshHealthReceipt"] != false || current["credentialConfigured"] != true {
		t.Fatal("current exact pins or missing receipt diagnostic invalid")
	}
	if readback("agt_foreign01", connection.Version)["scopeFound"] != false {
		t.Fatal("foreign agent entered exact connection scope")
	}
	if readback(agent.Ref, connection.Version+1)["connectionVersionMatches"] != false {
		t.Fatal("connection OCC mismatch hidden")
	}
	connection = *execute(command.TestConnection, "probe", &connection.Version, command.ConnectionInput{Ref: connection.Ref}).Connection
	claims, err := service.ClaimIntegrationConnectionTests(ctx, gateway, "readback-fixture", 1)
	if err != nil || len(claims) != 1 {
		t.Fatal("synthetic owner test claim missing", err)
	}
	item := claims[0]
	result, err := service.Execute(ctx, command.Command{Kind: command.CompleteConnectionTest, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: "readback-complete"}, Payload: command.IntegrationConnectionTestInput{TestRef: stringMap(item, "testRef"), LeaseRef: stringMap(item, "leaseRef"), Fence: stringMap(item, "fence"), Generation: item["generation"].(int64), Success: true}})
	if err != nil || result.Connection == nil {
		t.Fatal("synthetic health completion failed", err)
	}
	connection = *result.Connection
	current = readback(agent.Ref, connection.Version)
	if current["freshHealthReceipt"] != true {
		t.Fatal("fresh exact owner probe was not accepted as current health")
	}
	if _, err := pool.Exec(ctx, queryManagedMCPReadbackRecredential, pgx.StrictNamedArgs{"connection_ref": connection.Ref}); err != nil {
		t.Fatal("prepare next synthetic credential revision")
	}
	connection, err = service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	current = readback(agent.Ref, connection.Version)
	if current["freshHealthReceipt"] != false {
		t.Fatal("receipt ignored credential revision drift")
	}
	if _, err := pool.Exec(ctx, queryManagedMCPReadbackStale, pgx.StrictNamedArgs{"connection_ref": connection.Ref}); err != nil {
		t.Fatal("prepare synthetic stale receipt")
	}
	current = readback(agent.Ref, connection.Version)
	if current["freshHealthReceipt"] != false || current["pendingWithinWindow"] != false {
		t.Fatal("stale receipt became readiness or pending authority")
	}
	encoded, err := json.Marshal(current)
	if err != nil || strings.Contains(string(encoded), "credentialSHA256") || strings.Contains(string(encoded), "input_snapshot") || strings.Contains(string(encoded), "base_url") || strings.Contains(string(encoded), "secret_ref") {
		t.Fatal("readback leaked forbidden content")
	}
}
