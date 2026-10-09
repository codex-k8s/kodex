package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeFileCatalogFailureTx struct {
	pgx.Tx
	err error
}

type runtimeFileCatalogFailureRow struct{ err error }

func (row runtimeFileCatalogFailureRow) Scan(...any) error { return row.err }

func (tx runtimeFileCatalogFailureTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return runtimeFileCatalogFailureRow{err: tx.err}
}

func TestRuntimeFileCatalogDatabaseFailureIsNotEligibility(t *testing.T) {
	for _, capabilities := range [][]string{{}, {runtimecontract.ArtifactCapability}} {
		cause := &pgconn.PgError{Code: "42883", Message: "undefined function"}
		err := captureRuntimeFileCatalog(t.Context(), runtimeFileCatalogFailureTx{err: cause}, scope{},
			map[string]any{"projectRef": "prj_fixture", "capabilities": capabilities}, runtimecontract.RuntimeContextSnapshot{})
		var preserved *pgconn.PgError
		if !errors.Is(err, errs.ErrUnavailable) || !errors.As(err, &preserved) || preserved != cause || runtimeCandidateEligibilityFailure(err) {
			t.Fatal("database failure became terminal eligibility denial")
		}
	}
}

// Контур выполняет обычные owner-команды и реальные VFS queries одной новой
// disposable-базы. Provider, сеть и staging в этом сценарии не используются.
func TestWorkflowCoordinatorFilesComponent(t *testing.T) {
	ctx, stop := context.WithTimeout(t.Context(), 90*time.Second)
	defer stop()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// DDL faults разрешены только loopback administrator нового isolated fixture.
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	applicationConfig := pool.Config().ConnConfig
	if err != nil || adminConfig.User != "postgres" || adminConfig.Database != "postgres" ||
		adminConfig.Host != applicationConfig.Host || adminConfig.Port != applicationConfig.Port ||
		!strings.HasPrefix(applicationConfig.Database, "kodex_assistant_test_") {
		t.Fatal("fault injection requires the exact disposable fixture administrator")
	}
	adminConfig.Database = applicationConfig.Database
	fixtureAdmin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect disposable fixture administrator")
	}
	defer func() { _ = fixtureAdmin.Close(context.WithoutCancel(ctx)) }()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if err = repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64),
		RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute,
		AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles",
		DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey:       []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err = repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	execute := func(kind command.Kind, key string, payload any, version *int64) command.Result {
		t.Helper()
		actor := owner
		if kind == command.ClaimExecution || kind == command.CompleteExecution || kind == command.DelegateExecution || kind == command.RecordRunToolCall {
			actor = worker
		}
		if kind == command.LaunchWorkflowExecution {
			actor = worker
			actor.Permission = "platform.runtime.execution.workflow.launch"
		}
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{
			IdempotencyKey: "coordinator-files-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s/%s: %v", kind, key, err)
		}
		return result
	}
	claim := func(key, run string) map[string]any {
		t.Helper()
		items := execute(command.ClaimExecution, key, command.LeaseInput{WorkloadInstance: "coordinator-files-fixture", Limit: 1}, nil).RuntimeItems
		if len(items) != 1 || stringMap(items[0], "runRef") != run {
			t.Fatalf("unexpected claim %s: %v", key, items)
		}
		return items[0]
	}
	completion := func(lease map[string]any, text string) command.CompleteExecutionInput {
		result := command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"),
			Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Verified fixture", Usage: turnUsageFixture()}
		if text != "" {
			digest := sha256.Sum256([]byte(text))
			result.Artifacts = []command.CompletedArtifact{{FileName: "result.txt", MediaType: "text/plain",
				Content: []byte(text), SizeBytes: int64(len(text)), SHA256: hex.EncodeToString(digest[:])}}
		}
		return result
	}
	complete := func(key string, lease map[string]any, text string) command.Result {
		return execute(command.CompleteExecution, key, completion(lease, text), nil)
	}
	project := execute(command.CreateProject, "project", command.ProjectInput{Name: "Coordinator files", Language: "en"}, nil).Project
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "files-coordinator", "Coordinator")
	coordinator = *execute(command.ChangeAgentCapability, "delegate", command.AgentBindingInput{
		AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}, &coordinator.Version).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "files-specialist", "Specialist")
	for _, capability := range []string{runtimecontract.ArtifactCapability, "platform.run.launch"} {
		specialist = *execute(command.ChangeAgentCapability, capability, command.AgentBindingInput{
			AgentRef: specialist.Ref, BindingRef: capability, Enabled: true}, &specialist.Version).Agent
	}
	publish := func(key string, steps []entity.WorkflowStep) *entity.Workflow {
		draft := entity.WorkflowVersion{Name: key, Purpose: "Verified received results", CoordinatorAgentRef: coordinator.Ref,
			Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "Complete published steps", ResultSchema: map[string]any{}, Steps: steps}
		result := execute(command.CreateWorkflow, key+"-create", command.WorkflowInput{ProjectRef: project.Ref, Name: key,
			Purpose: draft.Purpose, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}, nil).Workflow
		result = execute(command.ValidateWorkflow, key+"-validate", command.WorkflowInput{Ref: result.Ref}, &result.Version).Workflow
		return execute(command.PublishWorkflow, key+"-publish", command.WorkflowInput{Ref: result.Ref}, &result.Version).Workflow
	}
	step := entity.WorkflowStep{Key: "first", Position: 1, Name: "First", AgentRef: specialist.Ref,
		Instructions: "Produce an exact result", ExpectedResult: "Result artifact", TimeoutSeconds: 900,
		RequiredCapabilityKeys: []string{runtimecontract.ArtifactCapability, "platform.run.launch"}}
	nested := publish("nested", []entity.WorkflowStep{step})
	second := step
	second.Key, second.Name, second.Position = "second", "Second", 2
	workflow := publish("outer", []entity.WorkflowStep{step, second})
	// Чужой Run того же проекта имеет реальный CLEAN AGENT_RESULT.
	foreign := execute(command.LaunchRun, "foreign", command.LaunchRunInput{ProjectRef: project.Ref,
		Target: entity.RunTarget{Type: "AGENT", Ref: specialist.Ref}, Task: "Produce unrelated output"}, nil).Run
	foreignResult := complete("foreign-complete", claim("foreign-claim", foreign.Ref), "foreign body").Run.ArtifactRefs[0]
	outer := execute(command.LaunchRun, "outer", command.LaunchRunInput{ProjectRef: project.Ref,
		Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}, Task: "Read received child results"}, nil).Run
	delegate := func(key string, lease map[string]any, stepKey string) *entity.Run {
		return execute(command.DelegateExecution, key, command.DelegateInput{LeaseRef: stringMap(lease, "leaseRef"),
			Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"),
			TargetAgentRef: specialist.Ref, WorkflowStepKey: stepKey, Task: "Produce verified output"}, nil).Run
	}
	manifestReader := runtimeFilesTestPrincipal(t, ctx, repository, "manifest")
	metadataReader := runtimeFilesTestPrincipal(t, ctx, repository, "metadata")
	bodyReader := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller",
		Operation: "platform.runtime.execution.artifact.read"}, "runtime-controller")
	manifest := func(lease map[string]any, wanted int) []entity.ExecutionFileDescriptor {
		t.Helper()
		capabilities := runtimeRevisionStringSlice(lease["capabilities"])
		if !slices.Equal(capabilities, []string{"platform.run.delegate"}) || len(runtimeRevisionGrants(lease["integrationGrants"])) != 0 {
			t.Fatal("intrinsic read widened coordinator capabilities or integration grants")
		}
		catalog := lease["fileCatalog"].(runtimecontract.RuntimeFileCatalog)
		if !slices.Equal(catalog.Purposes, []string{runtimecontract.FilePurposeRunResult}) {
			t.Fatal("intrinsic grant escaped RUN_RESULT")
		}
		execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
		page, err := service.GetExecutionFileManifest(ctx, manifestReader, execution, query.Page{Size: 100})
		if err != nil || len(page.Items) != wanted || page.Total != int64(wanted) {
			t.Fatalf("manifest count=%d total=%d err=%v", len(page.Items), page.Total, err)
		}
		for _, file := range page.Items {
			if file.ArtifactRef == foreignResult {
				t.Fatal("foreign root result entered coordinator catalog")
			}
		}
		return page.Items
	}
	readFile := func(lease map[string]any, file entity.ExecutionFileDescriptor, wanted string) {
		t.Helper()
		execution := runtimeFilesTestContext(t, lease, runtimecontract.FilePurposeRunResult)
		exact := query.ExecutionFileRef{EntryRef: file.EntryRef, ArtifactRef: file.ArtifactRef, Revision: file.Revision, Digest: file.Digest}
		if _, err := service.GetExecutionFileMetadata(ctx, metadataReader, execution, exact); err != nil {
			t.Fatal(err)
		}
		download, err := service.ReadExecutionArtifact(ctx, bodyReader, execution.LeaseRef, execution.Fence, execution.Generation, file.ArtifactRef)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(download.Reader)
		closeErr := download.Reader.Close()
		if readErr != nil || closeErr != nil || string(body) != wanted {
			t.Fatal("exact full body differs")
		}
		wrong := execution
		wrong.Generation++
		if _, err := service.GetExecutionFileMetadata(ctx, metadataReader, wrong, exact); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("stale generation was accepted", err)
		}
		wrong = execution
		wrong.Purpose = runtimecontract.FilePurposeProject
		if _, err := service.GetExecutionFileManifest(ctx, manifestReader, wrong, query.Page{Size: 1}); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("PROJECT purpose was accepted", err)
		}
		if _, err := service.ReadExecutionArtifact(ctx, bodyReader, execution.LeaseRef, execution.Fence, execution.Generation, foreignResult); !errors.Is(err, errs.ErrNotFound) {
			t.Fatal("foreign body accepted", err)
		}
	}
	// Отсутствующая миграция не является отказом authority и не закрывает Run.
	beforeFailure, err := service.GetRun(ctx, owner, outer.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixtureAdmin.Exec(ctx, `ALTER FUNCTION control_plane.runtime_file_coordinator(uuid,uuid,uuid,uuid,uuid) RENAME TO fixture_unavailable_coordinator`); err != nil {
		t.Fatal(err)
	}
	_, claimErr := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "coordinator-files-database-unavailable"},
		Payload:  command.LeaseInput{WorkloadInstance: "coordinator-files-fixture", Limit: 1}})
	if _, err = fixtureAdmin.Exec(ctx, `ALTER FUNCTION control_plane.fixture_unavailable_coordinator(uuid,uuid,uuid,uuid,uuid) RENAME TO runtime_file_coordinator`); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(claimErr, errs.ErrUnavailable) || runtimeCandidateEligibilityFailure(claimErr) {
		t.Fatal("missing migration became terminal graph failure", claimErr)
	}
	afterFailure, err := service.GetRun(ctx, owner, outer.Ref)
	if err != nil || beforeFailure.State != afterFailure.State || beforeFailure.Version != afterFailure.Version {
		t.Fatal("SQL infrastructure error mutated candidate graph")
	}
	initial := claim("initial", outer.Ref)
	manifest(initial, 0)
	// READ не даёт права опубликовать даже маленький собственный результат.
	_, err = service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "coordinator-files-denied-write"}, Payload: completion(initial, "unauthorized")})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("coordinator gained artifact WRITE", err)
	}
	first := delegate("first-delegate", initial, "first")
	complete("initial-complete", initial, "")
	firstText := strings.Repeat("verified first result\n", 1800)
	firstLease := claim("first-claim", first.Ref)
	firstCompleted := complete("first-complete", firstLease, firstText)
	complete("first-complete", firstLease, firstText)
	callback := claim("first-callback", outer.Ref)
	files := manifest(callback, 1)
	if files[0].ArtifactRef != firstCompleted.Run.ArtifactRefs[0] {
		t.Fatal("wrong received result")
	}
	readFile(callback, files[0], firstText)
	// Только fixture имитирует legacy receipt до новой миграции; транзакция
	// откатывает отключение trigger и NULL, production backfill отсутствует.
	legacy, err := fixtureAdmin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(ctx, `ALTER TABLE control_plane.callback_receipts DISABLE TRIGGER protect_callback_result_snapshot`); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.Exec(ctx, `UPDATE control_plane.callback_receipts SET result_snapshot=NULL WHERE child_run_id=(SELECT id FROM control_plane.runs WHERE ref=$1)`, first.Ref); err != nil {
		t.Fatal(err)
	}
	var legacyVisible int64
	if err = legacy.QueryRow(ctx, `SELECT count(*) FROM control_plane.runtime_file_visible_entries entry JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id WHERE catalog.ref=$1`, callback["fileCatalog"].(runtimecontract.RuntimeFileCatalog).Ref).Scan(&legacyVisible); err != nil || legacyVisible != 0 {
		t.Fatal("legacy receipt without pins granted intrinsic READ", err)
	}
	if err = legacy.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	execution := runtimeFilesTestContext(t, callback, runtimecontract.FilePurposeRunResult)
	activity := command.RunToolCallInput{LeaseRef: execution.LeaseRef, Fence: execution.Fence, Generation: execution.Generation,
		CallRef: "tcl_coordinatorfiles1", Tool: runtimecontract.FileToolRead, GrantRef: execution.CatalogRef,
		SafeParameters: map[string]any{"purpose": execution.Purpose}, State: "SUCCEEDED", Revision: 2, DurationMS: 1, SafeResult: "read_file:completed"}
	if result := execute(command.RecordRunToolCall, "read-activity", activity, nil); result.Event.ToolCall.CapabilityRef != "" {
		t.Fatal("read activity adopted a write capability")
	}
	var snapshot string
	if err = pool.QueryRow(ctx, `SELECT result_snapshot::text FROM control_plane.callback_receipts WHERE child_run_id=(SELECT id FROM control_plane.runs WHERE ref=$1)`, first.Ref).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE control_plane.callback_receipts SET result_snapshot=result_snapshot WHERE child_run_id=(SELECT id FROM control_plane.runs WHERE ref=$1)`, first.Ref); err == nil {
		t.Fatal("callback snapshot was mutable")
	}
	secondRun := delegate("second-delegate", callback, "second")
	complete("first-callback-complete", callback, "")
	secondLease := claim("second-claim", secondRun.Ref)
	nestedRun := execute(command.LaunchWorkflowExecution, "nested-launch", workflowCatalogLaunchInput(t, service, worker, secondLease, nested.Ref, "Produce nested result"), nil).Run
	complete("second-wait", secondLease, "")
	nestedInitial := claim("nested-initial", nestedRun.Ref)
	nestedChild := delegate("nested-delegate", nestedInitial, "first")
	complete("nested-initial-complete", nestedInitial, "")
	nestedText := "verified nested result"
	complete("nested-child-complete", claim("nested-child-claim", nestedChild.Ref), nestedText)
	nestedCallback := claim("nested-callback", nestedRun.Ref)
	nestedFiles := manifest(nestedCallback, 1)
	readFile(nestedCallback, nestedFiles[0], nestedText)
	complete("nested-callback-complete", nestedCallback, "")
	complete("second-resumed-complete", claim("second-resumed", secondRun.Ref), "")
	final := claim("final-callback", outer.Ref)
	all := manifest(final, 2)
	for _, file := range all {
		text := nestedText
		if file.ArtifactRef == files[0].ArtifactRef {
			text = firstText
		}
		readFile(final, file, text)
	}
	// Текущее состояние файла проверяется до каждой страницы и body query.
	finalContext := runtimeFilesTestContext(t, final, runtimecontract.FilePurposeRunResult)
	firstExact := query.ExecutionFileRef{EntryRef: files[0].EntryRef, ArtifactRef: files[0].ArtifactRef, Revision: files[0].Revision, Digest: files[0].Digest}
	for _, file := range all {
		if file.ArtifactRef == files[0].ArtifactRef {
			firstExact.EntryRef = file.EntryRef
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE control_plane.artifacts SET scan_state='QUARANTINED' WHERE ref=$1`, firstExact.ArtifactRef); err != nil {
		t.Fatal(err)
	}
	manifest(final, 1)
	if _, err = service.GetExecutionFileMetadata(ctx, metadataReader, finalContext, firstExact); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("quarantined pinned artifact retained metadata", err)
	}
	if _, err = service.ReadExecutionArtifact(ctx, bodyReader, finalContext.LeaseRef, finalContext.Fence, finalContext.Generation, firstExact.ArtifactRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("quarantined pinned artifact retained body", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE control_plane.artifacts SET scan_state='CLEAN' WHERE ref=$1`, firstExact.ArtifactRef); err != nil {
		t.Fatal(err)
	}
	manifest(final, 2)
	currentFile, err := service.GetArtifact(ctx, owner, firstExact.ArtifactRef)
	if err != nil {
		t.Fatal(err)
	}
	impact, err := service.GetArtifactImpact(ctx, owner, firstExact.ArtifactRef, "DELETE")
	if err != nil {
		t.Fatal(err)
	}
	deleted := execute(command.DeleteArtifact, "delete-result", command.ArtifactLifecycleInput{ArtifactRef: firstExact.ArtifactRef, ImpactDigest: impact.Digest}, &currentFile.Version).Artifact
	manifest(final, 1)
	if deleted == nil {
		t.Fatal("deleted artifact receipt missing")
	}
	execute(command.RestoreArtifact, "restore-result", command.ArtifactLifecycleInput{ArtifactRef: deleted.Ref}, &deleted.Version)
	// Restore увеличил version: прежний immutable pin не обновляется от summary.
	manifest(final, 1)
	if _, err = service.GetExecutionFileMetadata(ctx, metadataReader, finalContext, firstExact); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("restored version rejoined stale result pin", err)
	}
	var replaySnapshot string
	if err = pool.QueryRow(ctx, `SELECT result_snapshot::text FROM control_plane.callback_receipts WHERE child_run_id=(SELECT id FROM control_plane.runs WHERE ref=$1)`, first.Ref).Scan(&replaySnapshot); err != nil || replaySnapshot != snapshot {
		t.Fatal("replay changed immutable result")
	}
	stale := runtimeFilesTestContext(t, callback, runtimecontract.FilePurposeRunResult)
	if _, err = service.GetExecutionFileManifest(ctx, manifestReader, stale, query.Page{Size: 1}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("completed lease retained READ", err)
	}
	// Отзыв текущей capability закрывает intrinsic activity и чтение до terminal.
	coordinator = *execute(command.ChangeAgentCapability, "revoke-delegate", command.AgentBindingInput{
		AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: false}, &coordinator.Version).Agent
	revoked := runtimeFilesTestContext(t, final, runtimecontract.FilePurposeRunResult)
	if _, err = service.GetExecutionFileManifest(ctx, manifestReader, revoked, query.Page{Size: 1}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("revoked coordinator retained READ", err)
	}
	currentRun, err := service.GetRun(ctx, owner, outer.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, "cancel", command.RunCommandInput{RunRef: outer.Ref}, &currentRun.Version)
	if _, err = service.ReadExecutionArtifact(ctx, bodyReader, revoked.LeaseRef, revoked.Fence, revoked.Generation, files[0].ArtifactRef); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("terminal root retained body READ", err)
	}
}
