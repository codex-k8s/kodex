package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryRoleImageOwnerRetrySnapshot reads only the disposable fixture's owner effects and opaque
// state fingerprints, never receipt contents, claim tokens or external credentials.
// queryRoleImageOwnerRetrySnapshot читает только owner effects и непрозрачные fingerprints
// disposable fixture, без содержимого квитанций, claim tokens и внешних credentials.
//
//go:embed testdata/sql/role_image_owner_retry_snapshot.sql
var queryRoleImageOwnerRetrySnapshot string

// roleImageOwnerRetryCounts compares committed effects and aggregate state across rollback and
// exact replay; fingerprints include versions, claim closure and managed recipe lineage.
// roleImageOwnerRetryCounts сравнивает committed effects и состояния при rollback и exact replay;
// fingerprints включают versions, закрытие claim и managed recipe lineage.
type roleImageOwnerRetryCounts struct {
	builds, reports, receipts, events, sequence int64
	recipes, artifacts, attempts                string
}

// roleImageOwnerRetrySnapshot fails closed on an unreadable isolated state; only metadata is
// retained, so test failures cannot print tokens or serialized owner receipts.
// roleImageOwnerRetrySnapshot закрыто останавливает тест при недоступном isolated state; хранит
// только metadata, поэтому failures не печатают tokens либо serialized owner receipts.
func roleImageOwnerRetrySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) roleImageOwnerRetryCounts {
	t.Helper()
	var result roleImageOwnerRetryCounts
	if err := pool.QueryRow(ctx, queryRoleImageOwnerRetrySnapshot).Scan(&result.builds, &result.reports,
		&result.receipts, &result.events, &result.sequence, &result.recipes, &result.artifacts, &result.attempts); err != nil {
		t.Fatal("read isolated owner snapshot")
	}
	return result
}

// roleImageOwnerRetryFixture creates one native scoped recipe and completed synthetic build, then
// returns exact owner REQUEST_BUILD and live admission inputs. No BuildKit, registry or scan runs.
// roleImageOwnerRetryFixture создаёт один native scoped recipe и завершённую synthetic build,
// возвращая точные owner REQUEST_BUILD и live admission inputs. BuildKit, registry и scan не запускаются.
func roleImageOwnerRetryFixture(t *testing.T, scopeKind, verdict string) (context.Context, *pgxpool.Pool, *Repository, roleimagerepo.ManageInput, roleimagerepo.AdmissionRecordInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	pool := newIsolatedAssistantComponentPool(t, ctx)
	t.Cleanup(pool.Close)
	r, service, owner, resolved := newRoleImageAdmissionComponentFixture(t, ctx, pool)
	_, recipe := promotionComponentCatalog(t)
	input := roleimagerepo.ManageInput{Principal: resolved, ScopeKind: scopeKind, Action: "CREATE",
		Name: "Synthetic owner retry", Recipe: recipe, Mutation: roleImageTestMutation("owner-retry-create", "CREATE", nil)}
	if scopeKind == "PROJECT" {
		project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "owner-retry-project"}, Payload: command.ProjectInput{Name: "Synthetic retry", Language: "en"}})
		if err != nil || project.Project == nil {
			t.Fatal("create isolated project")
		}
		agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "owner-retry-agent", "Synthetic role")
		input.ProjectRef, input.RoleDefinitionRef = project.Project.Ref, agent.RoleDefinitionRef
	}
	created, err := r.manageScopedRoleImage(ctx, input)
	if err != nil || created.Build == nil {
		t.Fatal("create native scoped recipe")
	}
	seedPromotionArtifact(t, ctx, r, resolved, created.Recipe, *created.Build, "PENDING")
	worker := resolved
	worker.CallerWorkload, worker.Permission = "image-admission", "platform.role-images.admission.claim"
	claim, err := r.ClaimAdmission(ctx, worker, "owner-retry-claim")
	if err != nil || claim.Artifact.Ref == "" {
		t.Fatal("claim native admission")
	}
	a := claim.Artifact
	record := roleimagerepo.AdmissionRecordInput{Principal: worker, IdempotencyKey: "owner-retry-record",
		ArtifactRef: a.Ref, ClaimToken: claim.ClaimToken, ExpectedVersion: a.Version, ExpectedFence: claim.Fence,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt,
		ManifestDigest: a.ManifestDigest, ImmutableBuildSHA256: a.ImmutableBuildSHA256, ProvenanceSHA256: a.ProvenanceSHA256,
		PolicyRevision: a.PolicyRevision, PolicySHA256: a.PolicySHA256, Verdict: verdict, SBOMSHA256: strings.Repeat("1", 64),
		SignatureIdentity: "synthetic-owner", SignatureSHA256: strings.Repeat("3", 64),
		AdmissionReceiptSHA256: strings.Repeat("4", 64), AdmissionReceiptOCIManifestDigest: "sha256:" + strings.Repeat("5", 64)}
	record.ToolInventoryJSON, record.ToolInventorySHA256 = imageInventoryFixture(a)
	record.VulnerabilityReportJSON, record.VulnerabilityReportProjectionSHA256, record.VulnerabilityEvidenceSHA256 = imageRiskReportFixture(t, a, record.SBOMSHA256, verdict == "REJECTED")
	version := int64(created.Recipe.Version)
	input.Action, input.RecipeRef = "REQUEST_BUILD", created.Recipe.Ref
	input.Mutation = roleImageTestMutation("owner-retry-request", "REQUEST_BUILD", &version)
	return ctx, pool, r, input, record
}

// roleImageOwnerRetryInvoke enters the public scoped manage methods or the admission owner method;
// it never retries on the caller side and retains exact response bytes for replay comparison.
// roleImageOwnerRetryInvoke вызывает public scoped manage либо admission owner method; caller-side
// retry отсутствует, точный ответ сохраняется для сравнения replay.
func roleImageOwnerRetryInvoke(ctx context.Context, r *Repository, operation string, manage roleimagerepo.ManageInput, record roleimagerepo.AdmissionRecordInput) (any, error) {
	if operation == "record" {
		return r.RecordAdmission(ctx, record)
	}
	if manage.ScopeKind == "ORGANIZATION" {
		return r.ManageOrganization(ctx, manage)
	}
	return r.Manage(ctx, manage)
}

// TestRoleImageOwnerTransactionRetryComponent injects a real SQLSTATE at installation sequence
// append after owner writes, checks both scopes and verdicts, and proves one committed result and
// exact response-loss replay. A failed SQL attempt must leave no duplicate report/build/event.
// TestRoleImageOwnerTransactionRetryComponent внедряет настоящий SQLSTATE на installation sequence
// append после owner writes, проверяет обе области и вердикта, один committed result и exact
// replay после потери ответа. Неуспешная попытка не оставляет duplicate report/build/event.
func TestRoleImageOwnerTransactionRetryComponent(t *testing.T) {
	for _, state := range []string{"40001", "40P01"} {
		for _, scopeKind := range []string{"PROJECT", "ORGANIZATION"} {
			for _, operation := range []string{"manage", "record-ACCEPTED", "record-REJECTED"} {
				t.Run(state+"/"+scopeKind+"/"+operation, func(t *testing.T) {
					verdict := strings.TrimPrefix(operation, "record-")
					ctx, pool, r, manage, record := roleImageOwnerRetryFixture(t, scopeKind, verdict)
					kind := strings.Split(operation, "-")[0]
					before := roleImageOwnerRetrySnapshot(t, ctx, pool)
					roleImageWorkerRetryInject(t, ctx, pool, state, 1, kind)
					result, err := roleImageOwnerRetryInvoke(ctx, r, kind, manage, record)
					if err != nil {
						t.Fatalf("owner did not recover native %s rollback: %T", state, err)
					}
					wireResult, marshalErr := json.Marshal(result)
					if marshalErr != nil {
						t.Fatal("encode owner result")
					}
					after := roleImageOwnerRetrySnapshot(t, ctx, pool)
					buildDelta, reportDelta := int64(0), int64(1)
					if kind == "manage" {
						buildDelta, reportDelta = 1, 0
					}
					if roleImageWorkerRetryAttempts(t, ctx, pool) != 2 || after.builds != before.builds+buildDelta ||
						after.reports != before.reports+reportDelta || after.receipts != before.receipts+1 ||
						after.events != before.events+1 || after.sequence != before.sequence+1 {
						t.Fatal("rollback left duplicate or incomplete owner effects")
					}
					manage.Principal.CorrelationRef = "fresh-transport"
					replay, err := roleImageOwnerRetryInvoke(ctx, r, kind, manage, record)
					wireReplay, marshalErr := json.Marshal(replay)
					if err != nil || marshalErr != nil || !bytes.Equal(wireResult, wireReplay) || roleImageOwnerRetrySnapshot(t, ctx, pool) != after || roleImageWorkerRetryAttempts(t, ctx, pool) != 2 {
						t.Fatalf("exact replay mismatch: errorClass=%T invalidIntent=%t unequalResponse=%t changedState=%t attempts=%d", err, errors.Is(err, errs.ErrIdempotencyReuse), !bytes.Equal(wireResult, wireReplay), roleImageOwnerRetrySnapshot(t, ctx, pool) != after, roleImageWorkerRetryAttempts(t, ctx, pool))
					}
				})
			}
		}
	}
}

// TestRoleImageOwnerTransactionRetryBoundsComponent proves exhaustion, permanent failure, stale
// version and cancelled caller leave the entire owner state unchanged and respect the attempt cap.
// TestRoleImageOwnerTransactionRetryBoundsComponent доказывает неизменность полного owner state
// при exhaustion, постоянном отказе, stale version и отмене caller с прежним пределом attempts.
func TestRoleImageOwnerTransactionRetryBoundsComponent(t *testing.T) {
	for _, operation := range []string{"manage", "record"} {
		scenarios := []string{"exhausted", "permanent", "stale-version", "cancelled", "foreign-tenant"}
		if operation == "record" {
			scenarios = append(scenarios, "wrong-token", "wrong-fence", "wrong-attempt", "wrong-policy")
		}
		for _, scenario := range scenarios {
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				ctx, pool, r, manage, record := roleImageOwnerRetryFixture(t, "PROJECT", "REJECTED")
				before := roleImageOwnerRetrySnapshot(t, ctx, pool)
				state, attempts, wanted := "40001", int64(3), errs.ErrUnavailable
				if scenario == "permanent" {
					state, attempts = "23505", 1
				}
				if scenario == "stale-version" {
					version := int64(999)
					manage.Mutation = roleImageTestMutation("owner-retry-stale", "REQUEST_BUILD", &version)
					record.ExpectedVersion = 999
					attempts, wanted = 0, errs.ErrVersionMismatch
				}
				if scenario == "foreign-tenant" {
					manage.Principal.AuthorityTenant, record.Principal.AuthorityTenant = "org_synthetic_foreign", "org_synthetic_foreign"
					attempts, wanted = 0, errs.ErrForbidden
				}
				if strings.HasPrefix(scenario, "wrong-") {
					attempts, wanted = 0, errs.ErrForbidden
					switch scenario {
					case "wrong-token":
						record.ClaimToken = "synthetic-invalid-token"
					case "wrong-fence":
						record.ExpectedFence++
					case "wrong-attempt":
						record.ExpectedAdmissionAttempt++
					case "wrong-policy":
						record.PolicySHA256 = strings.Repeat("0", 64)
						if record.PolicySHA256 == r.roleImages.PolicySHA256 {
							record.PolicySHA256 = strings.Repeat("1", 64)
						}
					}
				}
				callContext := ctx
				if scenario == "cancelled" {
					cancelled, cancel := context.WithCancel(ctx)
					cancel()
					callContext = cancelled
					attempts = 0
				}
				roleImageWorkerRetryInject(t, ctx, pool, state, 99, operation)
				_, err := roleImageOwnerRetryInvoke(callContext, r, operation, manage, record)
				if !errors.Is(err, wanted) || roleImageWorkerRetryAttempts(t, ctx, pool) != attempts || roleImageOwnerRetrySnapshot(t, ctx, pool) != before {
					t.Fatal("failed owner command retried an unsafe outcome or persisted partial state")
				}
			})
		}
	}
}
