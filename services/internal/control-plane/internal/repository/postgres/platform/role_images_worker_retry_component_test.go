package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryRoleImageWorkerRetryDatabase is restricted to the exact isolated synthetic fault fixture.
// queryRoleImageWorkerRetryDatabase ограничен точной изолированной synthetic fault fixture.
//
//go:embed testdata/sql/role_image_worker_retry_database.sql
var queryRoleImageWorkerRetryDatabase string

// queryRoleImageWorkerRetrySnapshot is restricted to the exact isolated synthetic fault fixture.
// queryRoleImageWorkerRetrySnapshot ограничен точной изолированной synthetic fault fixture.
//
//go:embed testdata/sql/role_image_worker_retry_snapshot.sql
var queryRoleImageWorkerRetrySnapshot string

// queryRoleImageWorkerRetryAttempts is restricted to the exact isolated synthetic fault fixture.
// queryRoleImageWorkerRetryAttempts ограничен точной изолированной synthetic fault fixture.
//
//go:embed testdata/sql/role_image_worker_retry_attempts.sql
var queryRoleImageWorkerRetryAttempts string

// queryRoleImageWorkerRetryInstall is restricted to the exact isolated synthetic fault fixture.
// queryRoleImageWorkerRetryInstall ограничен точной изолированной synthetic fault fixture.
//
//go:embed testdata/sql/role_image_worker_retry_install.sql
var queryRoleImageWorkerRetryInstall string

// queryRoleImageWorkerRetryRemove is restricted to the exact isolated synthetic fault fixture.
// queryRoleImageWorkerRetryRemove ограничен точной изолированной synthetic fault fixture.
//
//go:embed testdata/sql/role_image_worker_retry_remove.sql
var queryRoleImageWorkerRetryRemove string

// roleImageWorkerRetryResult retains all native completion values for exact response-loss replay;
// no result, token or artifact is printed by the fixture.
// roleImageWorkerRetryResult сохраняет все native completion values для точного replay после
// потери ответа; fixture не печатает result, token либо artifact.
type roleImageWorkerRetryResult struct {
	build    entity.ImageBuild
	artifact entity.ImageArtifact
	token    string
}

// roleImageWorkerRetryCounts describes the persisted owner effects, excluding the deliberately
// nontransactional fault sequence. Only the test's isolated database is read.
// roleImageWorkerRetryCounts описывает сохранённые owner effects без намеренно нетранзакционной
// fault sequence. Читается только выделенная БД теста.
type roleImageWorkerRetryCounts struct {
	version, receipts, events, artifacts, audits, sequence int64
}

// roleImageWorkerRetryFixture creates one empty disposable database, bootstraps a synthetic
// owner and recipe, then claims and advances that exact build to context validation.
// roleImageWorkerRetryFixture создаёт пустую disposable БД, synthetic owner/recipe, затем
// захватывает именно эту сборку и доводит её до context validation.
func roleImageWorkerRetryFixture(t *testing.T, operation string) (context.Context, *pgxpool.Pool, *roleimageservice.Service, roleimagerepo.BuildLeaseInput, roleimagerepo.BuildCompletionInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	pool := newIsolatedAssistantComponentPool(t, ctx)
	t.Cleanup(pool.Close)
	repository := newNamespacedRoleImageComponentRepository(t, ctx, pool)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.organization.role-images.recipes.manage",
	}, "control-api-gateway")
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve synthetic owner")
	}
	catalog, specification := promotionComponentCatalog(t)
	service, err := roleimageservice.New(repository, catalog)
	if err != nil {
		t.Fatal("construct synthetic owner service")
	}
	created, err := repository.ManageOrganization(ctx, roleimagerepo.ManageInput{
		Principal: resolved, Action: "CREATE", Name: "Synthetic worker transaction retry", Recipe: specification,
		Mutation: roleImageTestMutation("worker-retry-create", "CREATE", nil),
	})
	if err != nil {
		t.Fatal("create synthetic recipe")
	}
	worker := owner
	worker.CallerWorkload, worker.Permission = "role-image-builder", "platform.role-images.builds.claim"
	claim, err := service.ClaimBuild(ctx, worker, "worker-retry-claim")
	if err != nil || claim.Build.Ref != created.Build.Ref {
		t.Fatal("claim exact synthetic build")
	}
	worker.Permission = "platform.role-images.builds.progress"
	lease := roleimagerepo.BuildLeaseInput{Principal: worker, IdempotencyKey: "worker-retry-context",
		BuildRef: claim.Build.Ref, LeaseToken: claim.LeaseToken, ExpectedVersion: claim.Build.Version,
		ExpectedAttempt: claim.Build.Attempt, ExpectedFence: claim.Fence}
	progress, err := service.ReportBuildProgress(ctx, roleimagerepo.BuildProgressInput{BuildLeaseInput: lease, Stage: "CONTEXT_VALIDATION", ProgressPercent: 15})
	if err != nil {
		t.Fatal("prepare context-validation fixture")
	}
	lease.ExpectedVersion = progress.Version
	lease.IdempotencyKey = "worker-retry-" + operation
	lease.Principal.Permission = "platform.role-images.builds." + operation
	return ctx, pool, service, lease, roleimagerepo.BuildCompletionInput{
		ManifestDigest:   "sha256:" + strings.Repeat("e", 64),
		StagingReference: repository.roleImages.StagingRepository + "@sha256:" + strings.Repeat("e", 64),
		ProvenanceSHA256: strings.Repeat("f", 64), ImmutableBuildSHA256: claim.Build.ImmutableBuildSHA256,
	}
}

// roleImageWorkerRetryInvoke executes exactly one owner method with the supplied immutable
// lease input; no BuildKit, registry write or caller-side retry exists in this fixture.
// roleImageWorkerRetryInvoke выполняет ровно один owner method с заданным immutable lease input;
// BuildKit, registry write и caller-side retry в fixture отсутствуют.
func roleImageWorkerRetryInvoke(ctx context.Context, service *roleimageservice.Service, operation string, lease roleimagerepo.BuildLeaseInput, completion roleimagerepo.BuildCompletionInput) (roleImageWorkerRetryResult, error) {
	var result roleImageWorkerRetryResult
	var err error
	switch operation {
	case "progress":
		result.build, err = service.ReportBuildProgress(ctx, roleimagerepo.BuildProgressInput{BuildLeaseInput: lease, Stage: "BASE_PULL", ProgressPercent: 35})
	case "renew":
		var renewed entity.ImageBuildClaim
		renewed, err = service.RenewBuild(ctx, lease)
		result.build, result.token = renewed.Build, renewed.LeaseToken
	case "complete":
		completion.BuildLeaseInput = lease
		result.build, result.artifact, err = service.CompleteBuild(ctx, completion)
	case "fail":
		result.build, err = service.FailBuild(ctx, roleimagerepo.BuildFailureInput{BuildLeaseInput: lease,
			ErrorCode: "SOLVE_FAILED", DiagnosticCode: "BUILD_GRAPH_REJECTED", DiagnosticSummary: "Synthetic build solve failed"})
	default:
		return result, errors.New("unknown synthetic operation")
	}
	return result, err
}

// roleImageWorkerRetryInject raises a controlled native SQLSTATE after the owner update and
// receipt insert, at the installation-sequence append (renewal has no event and faults at receipt insert).
// nextval survives rollback, so counts prove
// actual attempts; all trigger DDL and cleanup are restricted to the random fixture database.
// roleImageWorkerRetryInject вызывает controlled native SQLSTATE после owner update и receipt
// insert на installation-sequence append (renewal без event проверяется на receipt insert).
// nextval переживает rollback и доказывает attempts;
// trigger DDL и cleanup ограничены случайной fixture database.
func roleImageWorkerRetryInject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, state string, failures int, operation string) {
	t.Helper()
	if (state != "40001" && state != "40P01" && state != "23505") || failures < 1 || failures > 99 {
		t.Fatal("invalid synthetic fault parameters")
	}
	var database string
	if err := pool.QueryRow(ctx, queryRoleImageWorkerRetryDatabase).Scan(&database); err != nil || !strings.HasPrefix(database, "kodex_assistant_test_") {
		t.Fatal("fault injection requires an isolated fixture database")
	}
	admin := evidenceRetentionAdministrator(t, ctx, pool.Config().ConnConfig)
	target, action := "control_plane.installation", "UPDATE"
	if operation == "renew" {
		target, action = "control_plane.idempotency_receipts", "INSERT"
	}
	ddl := fmt.Sprintf(queryRoleImageWorkerRetryInstall, action, target, failures, state, pgx.Identifier{pool.Config().ConnConfig.User}.Sanitize())
	if _, err := admin.Exec(ctx, ddl); err != nil {
		t.Fatal("install isolated synthetic fault")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, fmt.Sprintf(queryRoleImageWorkerRetryRemove, target)); err != nil {
			t.Error("remove exact isolated fault objects")
		}
	})
}

// roleImageWorkerRetrySnapshot reads metadata counts without receipt bodies or lease material;
// comparison proves rollback and exact replay preserve the original owner effects.
// roleImageWorkerRetrySnapshot читает metadata counts без receipt bodies и lease material;
// сравнение доказывает сохранность owner effects при rollback и exact replay.
func roleImageWorkerRetrySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ref string) roleImageWorkerRetryCounts {
	t.Helper()
	var result roleImageWorkerRetryCounts
	err := pool.QueryRow(ctx, queryRoleImageWorkerRetrySnapshot, ref).Scan(&result.version, &result.receipts, &result.events, &result.artifacts, &result.audits, &result.sequence)
	if err != nil {
		t.Fatal("read isolated owner counts")
	}
	return result
}

// roleImageWorkerRetryAttempts returns the nontransactional trigger invocation count; absence
// of any invocation remains zero and is distinct from a rolled-back owner mutation.
// roleImageWorkerRetryAttempts возвращает нетранзакционное число вызовов trigger; отсутствие
// вызовов остаётся нулём и отличается от откатанной owner mutation.
func roleImageWorkerRetryAttempts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var attempts int64
	if err := pool.QueryRow(ctx, queryRoleImageWorkerRetryAttempts).Scan(&attempts); err != nil {
		t.Fatal("read synthetic attempt count")
	}
	return attempts
}

// TestRoleImageWorkerTransactionRetryComponent injects native serialization/deadlock failures
// after partial owner writes for all four worker mutations, then checks one committed effect and
// exact replay under a fresh transport correlation. It never starts an external build.
// TestRoleImageWorkerTransactionRetryComponent внедряет native serialization/deadlock failures
// после частичных owner writes для четырёх worker mutations, затем проверяет один commit и exact
// replay с новым transport correlation. Внешняя сборка не запускается.
func TestRoleImageWorkerTransactionRetryComponent(t *testing.T) {
	for _, state := range []string{"40001", "40P01"} {
		for _, operation := range []string{"progress", "renew", "complete", "fail"} {
			t.Run(state+"/"+operation, func(t *testing.T) {
				ctx, pool, service, lease, completion := roleImageWorkerRetryFixture(t, operation)
				before := roleImageWorkerRetrySnapshot(t, ctx, pool, lease.BuildRef)
				roleImageWorkerRetryInject(t, ctx, pool, state, 1, operation)
				result, err := roleImageWorkerRetryInvoke(ctx, service, operation, lease, completion)
				if err != nil {
					t.Fatalf("owner operation did not recover native %s rollback: %T", state, err)
				}
				if roleImageWorkerRetryAttempts(t, ctx, pool) != 2 {
					t.Fatal("expected one rollback and one successful transaction")
				}
				after := roleImageWorkerRetrySnapshot(t, ctx, pool, lease.BuildRef)
				artifactDelta, eventDelta := int64(0), int64(1)
				if operation == "renew" {
					eventDelta = 0
				}
				if operation == "complete" {
					artifactDelta = 1
				}
				if after.version != before.version+1 || after.receipts != before.receipts+1 || after.events != before.events+eventDelta || after.sequence != before.sequence+eventDelta || after.artifacts != before.artifacts+artifactDelta || after.audits != before.audits+artifactDelta {
					t.Fatal("rollback left duplicate or incomplete owner effects")
				}
				lease.Principal.CorrelationRef = "synthetic-fresh-transport-correlation"
				replay, err := roleImageWorkerRetryInvoke(ctx, service, operation, lease, completion)
				if err != nil || !reflect.DeepEqual(result, replay) || roleImageWorkerRetrySnapshot(t, ctx, pool, lease.BuildRef) != after || roleImageWorkerRetryAttempts(t, ctx, pool) != 2 {
					t.Fatal("exact replay changed response or persisted effects")
				}
			})
		}
	}
}

// TestRoleImageWorkerTransactionRetryBoundsComponent checks exhaustion, a permanent uniqueness
// failure and stale OCC on every worker operation. None may commit partial state or leak driver
// detail; authoritative version rejection must not reach the injected SQL write at all.
// TestRoleImageWorkerTransactionRetryBoundsComponent проверяет exhaustion, постоянный unique
// failure и stale OCC для каждого worker operation. Partial state и driver detail запрещены;
// authoritative version rejection вообще не должен достигать injected SQL write.
func TestRoleImageWorkerTransactionRetryBoundsComponent(t *testing.T) {
	for _, operation := range []string{"progress", "renew", "complete", "fail"} {
		for _, scenario := range []string{"exhausted", "permanent", "stale-version", "stale-attempt", "stale-fence", "wrong-token"} {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				ctx, pool, service, lease, completion := roleImageWorkerRetryFixture(t, operation)
				before := roleImageWorkerRetrySnapshot(t, ctx, pool, lease.BuildRef)
				state, attempts, wanted := "40001", int64(roleImageTransactionAttempts), errs.ErrUnavailable
				if scenario == "permanent" {
					state, attempts, wanted = "23505", 1, errs.ErrUnavailable
					if operation == "renew" {
						wanted = errs.ErrConflict
					}
				}
				if scenario == "stale-version" {
					lease.ExpectedVersion++
					attempts, wanted = 0, errs.ErrVersionMismatch
				}
				if scenario == "stale-attempt" {
					lease.ExpectedAttempt++
					attempts, wanted = 0, errs.ErrVersionMismatch
				}
				if scenario == "stale-fence" {
					lease.ExpectedFence++
					attempts, wanted = 0, errs.ErrVersionMismatch
				}
				if scenario == "wrong-token" {
					lease.LeaseToken = strings.Repeat("0", 64)
					attempts, wanted = 0, errs.ErrForbidden
				}
				roleImageWorkerRetryInject(t, ctx, pool, state, 99, operation)
				result, err := roleImageWorkerRetryInvoke(ctx, service, operation, lease, completion)
				if !errors.Is(err, wanted) || strings.Contains(err.Error(), "synthetic driver detail") || !reflect.DeepEqual(result, roleImageWorkerRetryResult{}) {
					t.Fatal("unexpected rejection, nonzero response or driver detail exposure")
				}
				if roleImageWorkerRetryAttempts(t, ctx, pool) != attempts || roleImageWorkerRetrySnapshot(t, ctx, pool, lease.BuildRef) != before {
					t.Fatal("retry exceeded budget or committed partial effects")
				}
			})
		}
	}
}
