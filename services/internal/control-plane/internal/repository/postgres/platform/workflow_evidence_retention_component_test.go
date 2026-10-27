package platform

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryWorkflowEvidenceRetentionProof observes private historical custody without granting a runtime reader new privileges.
// queryWorkflowEvidenceRetentionProof наблюдает private historical custody, не выдавая runtime reader новых прав.
//
//go:embed testdata/sql/workflow_evidence_retention_proof.sql
var queryWorkflowEvidenceRetentionProof string

// queryWorkflowEvidenceHeadLock holds only an actual disposable HEAD lock, without fabricating business state.
// queryWorkflowEvidenceHeadLock удерживает actual disposable HEAD lock, не создавая business state.
//
//go:embed testdata/sql/workflow_evidence_head_lock.sql
var queryWorkflowEvidenceHeadLock string

// queryWorkflowEvidenceDeadlineWait reads the actual immutable root deadline rather than changing its clock.
// queryWorkflowEvidenceDeadlineWait читает actual immutable root deadline, не изменяя часы исполнения.
//
//go:embed testdata/sql/workflow_evidence_deadline_wait.sql
var queryWorkflowEvidenceDeadlineWait string

// evidenceRetentionAdministrator restricts fault/observation access to the exact disposable native database.
// evidenceRetentionAdministrator ограничивает fault/observation access exact disposable native database.
func evidenceRetentionAdministrator(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
		t.Fatal("invalid disposable evidence database")
	}
	admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
		t.Fatal("invalid disposable evidence administrator")
	}
	admin.Database = runtime.Database
	conn, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		t.Fatal("connect disposable evidence administrator")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = conn.Close(cleanup)
	})
	return conn
}

// assertIssueRetention checks exact report/log revisions and rejects another captured file or a foreign revision ID.
// assertIssueRetention проверяет exact report/log revisions и отвергает другой capture либо foreign revision ID.
func (fixture *workflowEvidenceNativeFixture) assertIssueRetention(admin *pgx.Conn, report, log, unrelated entity.Artifact, want bool) {
	f := fixture.f
	f.t.Helper()
	var reportPin, logPin, otherPin, foreignRevision, retainedReport, retainedLog bool
	err := admin.QueryRow(f.ctx, queryWorkflowEvidenceRetentionProof, report.Ref, log.Ref, unrelated.Ref).Scan(&reportPin, &logPin, &otherPin, &foreignRevision, &retainedReport, &retainedLog)
	if err != nil || reportPin != want || logPin != want || otherPin || foreignRevision || want && (!retainedReport || !retainedLog) {
		f.t.Fatal("exact unresolved evidence retention mismatch")
	}
}

// deleteEvidence uses the ordinary owner impact/version path; retaining unresolved bytes does not prohibit soft deletion.
// deleteEvidence использует ordinary owner impact/version path; удержание unresolved bytes не запрещает soft deletion.
func (fixture *workflowEvidenceNativeFixture) deleteEvidence(file entity.Artifact) entity.Artifact {
	f := fixture.f
	f.t.Helper()
	current, err := f.service.GetArtifact(f.ctx, f.owner, file.Ref)
	if err != nil {
		f.t.Fatal("read evidence HEAD before deletion")
	}
	impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, file.Ref, "DELETE")
	if err != nil || !impact.Permitted {
		f.t.Fatal("evidence soft deletion unavailable")
	}
	result := f.execute(command.DeleteArtifact, fixture.key("delete-evidence"), &current.Version, command.ArtifactLifecycleInput{ArtifactRef: file.Ref, ImpactDigest: impact.Digest})
	if result.Artifact == nil {
		f.t.Fatal("evidence deletion did not return owner state")
	}
	return *result.Artifact
}

// checkIssueRetention proves pending/UNKNOWN custody through terminal root and grant changes, using actual owner commands.
// The UPDATE-lock counterexample and deleted-HEAD order prevent admission across a purge boundary; no business rows are seeded.
// checkIssueRetention доказывает pending/UNKNOWN custody после terminal root и grant changes actual owner commands.
// UPDATE-lock counterexample и deleted-HEAD order запрещают admission через purge boundary; business rows не создаются SQL.
func (fixture *workflowEvidenceNativeFixture) checkIssueRetention(mode string, lease map[string]any, report, log, unrelated entity.Artifact, input map[string]any) {
	f := fixture.f
	f.t.Helper()
	admin := evidenceRetentionAdministrator(f.t, f.ctx)
	if strings.HasSuffix(mode, "head-deleted") {
		report = fixture.deleteEvidence(report)
		if _, err := fixture.resolve(lease, "evidence-issue", "github.issue.create", input); err == nil {
			f.t.Fatal("deleted HEAD admitted an Issue intent")
		}
		fixture.assertIssueRetention(admin, report, log, unrelated, false)
		fixture.cancelEvidenceRoot()
		impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, report.Ref, "PURGE")
		if err != nil || !impact.Permitted {
			f.t.Fatal("purge-first fixture did not release ordinary retention")
		}
		state, err := f.service.PurgeArtifact(f.ctx, f.owner, value.Mutation{IdempotencyKey: fixture.key("purge-first"), ExpectedVersion: &report.Version}, report.Ref, impact.Digest)
		if err != nil || state != "PURGED" {
			f.t.Fatal("ordinary purge-first failed")
		}
		return
	}
	if strings.HasSuffix(mode, "head-lock") {
		tx, err := admin.Begin(f.ctx)
		if err != nil {
			f.t.Fatal("begin independent HEAD lock")
		}
		var id string
		if err := tx.QueryRow(f.ctx, queryWorkflowEvidenceHeadLock, report.Ref).Scan(&id); err != nil {
			f.t.Fatal("hold exact HEAD UPDATE lock")
		}
		_, denied := fixture.resolve(lease, "evidence-issue", "github.issue.create", input)
		if err := tx.Rollback(f.ctx); err != nil {
			f.t.Fatal("release independent HEAD lock")
		}
		if denied == nil {
			f.t.Fatal("HEAD NOWAIT admission ignored concurrent UPDATE lock")
		}
		fixture.assertIssueRetention(admin, report, log, unrelated, false)
	}
	var invocation map[string]any
	var err error
	if strings.HasSuffix(mode, "head-lock") {
		invocation = fixture.resolveIssueUnderHeadFence(admin, lease, report, input)
	} else {
		invocation, err = fixture.resolve(lease, "evidence-issue", "github.issue.create", input)
	}
	if err != nil {
		f.t.Fatal("admit exact retention Issue", completionOutcomeClass(err))
	}
	fixture.assertIssueRetention(admin, report, log, unrelated, true)
	unknown := strings.Contains(mode, "unknown") || strings.HasSuffix(mode, "head-lock")
	if unknown {
		if gateRef := stringMap(invocation, "gateRef"); gateRef != "" {
			gate, err := f.service.GetOwnerGate(f.ctx, f.owner, gateRef)
			if err != nil {
				f.t.Fatal("read retention effect Gate")
			}
			f.execute(command.ResolveOwnerGate, fixture.key("approve-retention"), &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Only the isolated native Issue effect."})
		}
		work := fixture.claimEffect(invocation)
		if strings.Contains(mode, "model-first") {
			fixture.complete(lease, "Issue outcome pending; no final evidence claim.")
		}
		_, err := f.service.Execute(f.ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: fixture.gateway, Mutation: value.Mutation{IdempotencyKey: fixture.key("unknown-retention")}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(work, "invocationRef"), LeaseRef: stringMap(work, "leaseRef"), Fence: stringMap(work, "fence"), Generation: runtimeRevisionMapInt64(work, "generation"), UnknownOutcome: true, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN"}})
		if err != nil {
			f.t.Fatal("persist actual unresolved Issue outcome", completionOutcomeClass(err))
		}
	}
	if strings.HasSuffix(mode, "unknown-revoke") {
		connection, err := f.service.GetIntegrationConnection(f.ctx, f.owner, fixture.connection.Ref)
		if err != nil {
			f.t.Fatal("read connection before retention revoke")
		}
		f.execute(command.ChangeIntegrationGrant, fixture.key("retention-revoke"), &connection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: f.agents["check"], CapabilityKey: "github.issue.create", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: false})
	}
	if strings.HasSuffix(mode, "unknown-expiry") {
		var remaining int64
		if err := admin.QueryRow(f.ctx, queryWorkflowEvidenceDeadlineWait, f.runRef).Scan(&remaining); err != nil || remaining < 0 || remaining > 50000 {
			f.t.Fatal("actual bounded evidence deadline unavailable")
		}
		timer := time.NewTimer(time.Duration(remaining+100) * time.Millisecond)
		select {
		case <-f.ctx.Done():
			timer.Stop()
			f.t.Fatal("evidence deadline wait exhausted")
		case <-timer.C:
		}
		if claims := f.claims(fixture.key("expire-evidence-root")); len(claims) != 0 {
			f.t.Fatal("expired evidence root issued fresh model work")
		}
		current, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
		if err != nil || current.State != "FAILED" {
			f.t.Fatal("actual execution expiry did not close root")
		}
	} else {
		fixture.cancelEvidenceRoot()
	}
	fixture.assertIssueRetention(admin, report, log, unrelated, unknown)
	state, err := f.service.GetIntegrationInvocation(f.ctx, f.worker, stringMap(invocation, "invocationRef"))
	expected := "CANCELLED"
	if unknown {
		expected = "UNKNOWN_OUTCOME"
	}
	if err != nil || stringMap(state, "state") != expected || stringMap(state, "effectReceiptRef") != "" {
		f.t.Fatal("terminal root reinterpreted unresolved native outcome")
	}
	for _, file := range []entity.Artifact{report, log} {
		deleted := fixture.deleteEvidence(file)
		impact, err := f.service.GetArtifactImpact(f.ctx, f.owner, file.Ref, "PURGE")
		if err != nil || impact.Permitted == unknown {
			f.t.Fatal("terminal evidence purge eligibility mismatch")
		}
		mutation := value.Mutation{IdempotencyKey: fixture.key("retention-purge"), ExpectedVersion: &deleted.Version}
		lifecycle, err := f.service.PurgeArtifact(f.ctx, f.owner, mutation, file.Ref, impact.Digest)
		if unknown {
			if !errors.Is(err, errs.ErrConflict) {
				f.t.Fatal("unresolved evidence purge did not close")
			}
		} else if err != nil || lifecycle != "PURGED" {
			f.t.Fatal("known terminal outcome kept obsolete evidence pins")
		}
	}
}

// cancelEvidenceRoot uses the current owner version and preserves external UNKNOWN facts.
// cancelEvidenceRoot использует current owner version и сохраняет external UNKNOWN facts.
func (fixture *workflowEvidenceNativeFixture) cancelEvidenceRoot() {
	f := fixture.f
	f.t.Helper()
	current, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil {
		f.t.Fatal("read evidence root before cancellation")
	}
	f.execute(command.CancelRun, fixture.key("cancel-retention-root"), &current.Version, command.RunCommandInput{RunRef: current.Ref, Reason: "Close the isolated native root without replaying an external Issue."})
}

// workflowEvidenceAdmissionPause blocks an exact report object read after its native HEAD SHARE
// lock, keeping the real owner transaction open for the opposite lock-order counterexample.
// workflowEvidenceAdmissionPause задерживает exact report object read после native HEAD SHARE
// lock, удерживая настоящую owner transaction для проверки обратного порядка блокировок.
type workflowEvidenceAdmissionPause struct {
	objectstorage.Store
	digest  string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Get preserves exact immutable bytes and releases its response body on cancellation.
// Get сохраняет exact immutable bytes и закрывает response body при cancellation.
func (store *workflowEvidenceAdmissionPause) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err != nil || object.Digest != store.digest {
		return object, err
	}
	store.once.Do(func() { close(store.entered) })
	select {
	case <-store.release:
		return object, nil
	case <-ctx.Done():
		_ = object.Body.Close()
		return objectstorage.Object{}, ctx.Err()
	}
}

// resolveIssueUnderHeadFence proves SHARE-before-UPDATE exclusion in the actual owner admission;
// the child is cancelled/joined before restoring the Store and observing committed retention.
// resolveIssueUnderHeadFence доказывает SHARE-before-UPDATE exclusion actual owner admission;
// child отменяется/join-ится до восстановления Store и чтения committed retention.
func (fixture *workflowEvidenceNativeFixture) resolveIssueUnderHeadFence(admin *pgx.Conn, lease map[string]any, report entity.Artifact, input map[string]any) map[string]any {
	f := fixture.f
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	original := f.repository.objects
	pause := &workflowEvidenceAdmissionPause{Store: original, digest: report.Digest, entered: make(chan struct{}), release: make(chan struct{})}
	f.repository.objects = pause
	// outcome transfers only the joined owner result and error to this fixture.
	// outcome передаёт только joined owner result и error этой оснастке.
	type outcome struct {
		value map[string]any
		err   error
	}
	completed := make(chan outcome, 1)
	go func() {
		value, err := f.service.ResolveIntegrationInvocation(ctx, f.worker, map[string]string{"run_ref": stringMap(lease, "runRef"), "node_ref": stringMap(lease, "nodeRef"), "connection_ref": fixture.connection.Ref, "capability_key": "github.issue.create", "idempotency_key": "evidence-issue"}, input)
		completed <- outcome{value, err}
	}()
	var lockErr error
	select {
	case <-pause.entered:
		tx, err := admin.Begin(ctx)
		if err != nil {
			lockErr = err
		} else {
			var id string
			err = tx.QueryRow(ctx, queryWorkflowEvidenceHeadLock, report.Ref).Scan(&id)
			var fault *pgconn.PgError
			if !errors.As(err, &fault) || fault.Code != "55P03" {
				lockErr = errs.ErrConflict
			}
			if rollback := tx.Rollback(ctx); rollback != nil {
				lockErr = rollback
			}
		}
	case <-ctx.Done():
		lockErr = ctx.Err()
	}
	close(pause.release)
	var result outcome
	select {
	case result = <-completed:
	case <-ctx.Done():
		cancel()
		joined := time.NewTimer(2 * time.Second)
		defer joined.Stop()
		select {
		case result = <-completed:
		case <-joined.C:
			f.t.Fatal("evidence admission child did not join")
		}
	}
	f.repository.objects = original
	if lockErr != nil || result.err != nil {
		f.t.Fatal("admission HEAD SHARE did not fence concurrent purge UPDATE", completionOutcomeClass(result.err))
	}
	return result.value
}
