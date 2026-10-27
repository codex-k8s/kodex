package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// organizationRuntimeImageScopeReadbackQuery reads system image/environment organization scope,
// current version/history count and one admitted project-image reference for cross-scope checks.
// organizationRuntimeImageScopeReadbackQuery читает область организации системного образа и
// окружения, текущую версию, число версий и ref одного допущенного образа проекта для межобластных
// проверок.
//
//go:embed testdata/sql/organization_runtime_image_scope_readback.sql
var organizationRuntimeImageScopeReadbackQuery string

// organizationRuntimeImageMarkCustomQuery marks only the fixture image with a synthetic custom
// signature identity; it does not perform image admission or signing.
// organizationRuntimeImageMarkCustomQuery помечает только тестовый образ искусственной
// идентичностью пользовательской подписи; не выполняет допуск либо подпись образа.
//
//go:embed testdata/sql/organization_runtime_image_mark_custom.sql
var organizationRuntimeImageMarkCustomQuery string

// testOrganizationRuntimeImageScope checks organization/project image isolation and preserves a
// synthetic custom organization image across bootstrap configuration drift inside a rolled-back
// transaction, restoring repository configuration.
// testOrganizationRuntimeImageScope проверяет изоляцию образов организации и проекта и сохранение
// искусственного пользовательского образа организации при смене bootstrap-конфигурации в
// откатываемой транзакции с восстановлением конфигурации repository.
func testOrganizationRuntimeImageScope(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct image scope service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "organization-image-scope-project"},
		Payload:  command.ProjectInput{Name: "Organization image scope fixture"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create image scope project: %v", err)
	}
	createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "organization-image-scope-agent", "Organization scope fixture agent")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin organization image scope: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var organizationID, artifactID, artifactRef, scopeKind, currentVersionID, projectID, projectArtifactRef string
	var scopeIsOrganization bool
	var versionCount int
	if err := tx.QueryRow(ctx, organizationRuntimeImageScopeReadbackQuery).Scan(
		&organizationID, &artifactID, &artifactRef, &scopeKind, &scopeIsOrganization,
		&currentVersionID, &versionCount, &projectID, &projectArtifactRef,
	); err != nil {
		t.Fatalf("read organization image scope: %v", err)
	}
	if scopeKind != "ORGANIZATION" || !scopeIsOrganization {
		t.Fatalf("system image scope is not organizational: kind=%s nullProject=%t", scopeKind, scopeIsOrganization)
	}
	if _, _, _, _, err := repository.resolveScopedRuntimeEnvironmentImage(ctx, tx, organizationID, "", "ORGANIZATION", artifactRef, nil); err != nil {
		t.Fatalf("resolve exact organization image: %v", err)
	}
	for _, candidate := range []struct{ name, projectID, scopeKind, artifactRef string }{
		{"organization image in project", projectID, "PROJECT", artifactRef},
		{"project image in organization", "", "ORGANIZATION", projectArtifactRef},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			if _, _, _, _, err := repository.resolveScopedRuntimeEnvironmentImage(ctx, tx, organizationID, candidate.projectID, candidate.scopeKind, candidate.artifactRef, nil); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("cross-scope image resolution: %v", err)
			}
		})
	}
	if _, err := tx.Exec(ctx, organizationRuntimeImageMarkCustomQuery, artifactID); err != nil {
		t.Fatalf("prepare custom image fixture: %v", err)
	}
	original := repository.roleImages
	next := original
	next.DefaultImageReference = "registry.invalid/kodex/roles/next@sha256:" + strings.Repeat("f", 64)
	if err := repository.ConfigureRoleImages(next); err != nil {
		t.Fatalf("configure next bootstrap image: %v", err)
	}
	defer func() {
		if err := repository.ConfigureRoleImages(original); err != nil {
			t.Errorf("restore bootstrap configuration: %v", err)
		}
	}()
	if err := repository.reconcileSystemAssistantRuntimeEnvironment(ctx, tx); err != nil {
		t.Fatalf("reconcile custom organization image: %v", err)
	}
	var retainedVersionID string
	var retainedVersionCount int
	if err := tx.QueryRow(ctx, organizationRuntimeImageScopeReadbackQuery).Scan(
		&organizationID, &artifactID, &artifactRef, &scopeKind, &scopeIsOrganization,
		&retainedVersionID, &retainedVersionCount, &projectID, &projectArtifactRef,
	); err != nil {
		t.Fatalf("read retained custom image: %v", err)
	}
	if retainedVersionID != currentVersionID || retainedVersionCount != versionCount {
		t.Fatalf("bootstrap replaced custom image: retainedVersion=%s expectedVersion=%s count=%d expectedCount=%d",
			retainedVersionID, currentVersionID, retainedVersionCount, versionCount)
	}
}
