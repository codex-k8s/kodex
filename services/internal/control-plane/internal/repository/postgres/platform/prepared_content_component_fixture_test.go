package platform

import (
	"context"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// newPreparedContentComponentService resolves the fixed conversation-create owner, constructs the
// service, reads its assistant, resolves the warm-report worker and reports the same desired
// revision READY, in that order. The caller supplies an already bootstrapped repository with
// observed warm evidence and retains its pool, deadline, object store and all test assertions.
// This synthetic report neither starts a runtime nor creates an execution claim.
// newPreparedContentComponentService разрешает фиксированного владельца создания беседы, создаёт
// сервис, читает его помощника, разрешает исполнителя warm-report и сообщает READY той же
// желаемой ревизии в таком порядке. Вызывающий передаёт уже инициализированный репозиторий с
// наблюдаемой warm-оснасткой и сохраняет пул, срок, хранилище объектов и все проверки теста.
// Синтетический отчёт не запускает runtime и не создаёт заявку на исполнение.
func newPreparedContentComponentService(t *testing.T, ctx context.Context, repository *Repository) (*platformservice.Service, value.Principal) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.conversations.create"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	warm := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report"}, "runtime-controller")
	if _, err := service.ReportWarmRuntime(ctx, warm, command.WarmRuntimeInput{WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY"}); err != nil {
		t.Fatal(err)
	}
	return service, owner
}

// insertPreparedContentComponentReceipt encodes the accepted antivirus result and inserts the
// exact object receipt for the existing three-byte prepared-content fixtures. It uses the caller's
// transaction and preserves both fatal errors; artifact creation, plan binding, adoption, commit
// and independent lifecycle assertions remain in each test. It does not upload or delete bytes.
// insertPreparedContentComponentReceipt кодирует принятый результат антивируса и записывает точную
// квитанцию объекта для существующих трёхбайтовых оснасток подготовленного содержимого. Использует
// транзакцию вызывающего и сохраняет обе фатальные ошибки; создание артефакта, привязка плана,
// принятие, commit и независимые проверки жизненного цикла остаются в каждом тесте. Не загружает
// и не удаляет данные объекта.
func insertPreparedContentComponentReceipt(t *testing.T, ctx context.Context, tx pgx.Tx, artifactID string, prepared command.PreparedArtifact) {
	t.Helper()
	scanJSON, err := artifactAntivirusJSON(prepared.Antivirus)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes,antivirus_report) VALUES ($1,$2,$3,$4,$5,3,$6)`, artifactID, prepared.ObjectKey, prepared.ObjectVersion, prepared.ObjectETag, prepared.Digest, scanJSON); err != nil {
		t.Fatal(err)
	}
}
