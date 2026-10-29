package platform

import (
	"context"
	"strings"
	"testing"

	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// workflowPDLCReadOrderFixture reuses the existing native autonomous-invocation component graph.
// It verifies SQL chronology on actual owner-created invocations, not the full PDLC profile or GitHub delivery.
// workflowPDLCReadOrderFixture использует существующий native component graph автономных invocations.
// Проверяет SQL-хронологию actual owner-created invocations, не полный PDLC профиль либо GitHub delivery.
type workflowPDLCReadOrderFixture struct {
	ctx                                    context.Context
	repository                             *Repository
	service                                *serviceplatform.Service
	pool                                   *pgxpool.Pool
	owner, worker, gateway                 value.Principal
	runRef, connectionRef, writeReceiptRef string
	lease                                  map[string]any
}

// receipt admits, claims and completes one invocation through real owner commands and the trusted
// gateway boundary. No database state or timestamps are inserted or repaired by the fixture.
// receipt допускает, захватывает и завершает invocation через настоящие owner commands и trusted
// gateway boundary. Оснастка не вставляет и не исправляет состояния либо timestamps в БД.
func (f workflowPDLCReadOrderFixture) receipt(t *testing.T, key, operation string, input, output map[string]any, provider string) string {
	t.Helper()
	invocation, err := f.service.ResolveIntegrationInvocation(f.ctx, f.worker,
		map[string]string{"run_ref": f.runRef, "node_ref": stringMap(f.lease, "nodeRef"), "connection_ref": f.connectionRef, "capability_key": operation, "idempotency_key": "pdlc-order-" + key}, input)
	if err != nil || stringMap(invocation, "state") != "READY" {
		t.Fatalf("native chronology invocation not admitted: operation=%s err=%v", operation, err)
	}
	claims, err := f.service.ClaimIntegrationInvocations(f.ctx, f.gateway, "pdlc-order-gateway", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(invocation, "invocationRef") {
		t.Fatal("native chronology invocation not claimed")
	}
	claim := claims[0]
	summary := string(asJSON(output))
	_, err = f.service.Execute(f.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: f.gateway,
		Mutation: value.Mutation{IdempotencyKey: "pdlc-order-complete-" + key}, Payload: command.IntegrationInvocationInput{
			InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"),
			Success: true, EffectKey: stringMap(claim, "effectKey"), InputDigest: stringMap(claim, "inputDigest"), ProviderEffectRef: provider, ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary)),
		}})
	if err != nil {
		t.Fatal("native chronology completion failed", err)
	}
	stored, err := f.service.GetIntegrationInvocation(f.ctx, f.gateway, stringMap(invocation, "invocationRef"))
	if err != nil || stringMap(stored, "state") != "SUCCEEDED" || stringMap(stored, "effectReceiptRef") == "" {
		t.Fatal("native chronology receipt not persisted")
	}
	return stringMap(stored, "effectReceiptRef")
}

// prove executes the production ordering query against real PostgreSQL timestamps. It accepts
// fresh serial reads and rejects a later write, a later head read, foreign owner and missing receipt.
// prove выполняет production запрос порядка по настоящим timestamps PostgreSQL. Допускает свежие
// последовательные чтения, отвергает позднюю запись, позднее чтение head, чужого owner и missing receipt.
func (f workflowPDLCReadOrderFixture) prove(t *testing.T) {
	t.Helper()
	sha := strings.Repeat("a", 40)
	headInput := map[string]any{"pull_request_number": 1}
	headOutput := map[string]any{"number": 1, "title": "Fixture", "body": "", "state": "open", "head": "feature", "base": "main", "sha": sha, "draft": false, "url": "https://github.com/fixture/repository/pull/1"}
	head := f.receipt(t, "head", "github.pull_request.read", headInput, headOutput, "github-pull:1")
	content := f.receipt(t, "content", "github.repository.content.read", map[string]any{"path": "requirements.md", "ref": sha},
		map[string]any{"path": "requirements.md", "type": "file", "sha": sha, "size": 4, "text": "Text", "commit_sha": sha,
			"offset_bytes": 0, "next_offset_bytes": 4, "eof": true, "source_digest": "sha256:" + workflowResultSHA([]byte("Text")), "chunk_digest": "sha256:" + workflowResultSHA([]byte("Text"))}, "github-content:"+sha)
	lateWrite := f.receipt(t, "late-write", "github.repository.content.update", map[string]any{"path": "requirements.md", "branch": "feature", "message": "Уточнить документ", "content_base64": "VGV4dA==", "sha": sha}, map[string]any{"sha": sha, "path": "requirements.md"}, "github-commit:"+sha)
	lateHead := f.receipt(t, "late-head", "github.pull_request.read", headInput, headOutput, "github-pull:1")
	principal, err := f.repository.ResolvePrincipal(f.ctx, f.owner)
	if err != nil {
		t.Fatal("resolve native chronology principal")
	}
	current, err := f.repository.resolveScope(f.ctx, principal)
	if err != nil {
		t.Fatal("resolve native chronology owner")
	}
	for _, mode := range []string{"ordered", "late-write", "late-head", "foreign-owner", "missing-receipt", "swapped-reads"} {
		pins := pgx.StrictNamedArgs{"organization_id": current.organizationID, "write_receipt_ref": f.writeReceiptRef, "head_receipt_ref": head, "content_receipt_ref": content}
		switch mode {
		case "late-write":
			pins["write_receipt_ref"] = lateWrite
		case "late-head":
			pins["head_receipt_ref"] = lateHead
		case "foreign-owner":
			pins["organization_id"] = "00000000-0000-4000-8000-000000000001"
		case "missing-receipt":
			pins["content_receipt_ref"] = "erc_missing000"
		case "swapped-reads":
			pins["head_receipt_ref"], pins["content_receipt_ref"] = content, head
		}
		var ordered bool
		if err := f.pool.QueryRow(f.ctx, queryWorkflowPDLCReadOrder, pins).Scan(&ordered); err != nil || ordered != (mode == "ordered") {
			t.Fatalf("native receipt read order mismatch: case=%s ordered=%t err=%v", mode, ordered, err)
		}
		t.Logf("PDLC native receipt chronology verified: case=%s ordered=%t", mode, ordered)
	}
}
