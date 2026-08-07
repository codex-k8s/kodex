// Package app содержит единственный composition root legacy-data-migration.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/backup"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/observability"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/planner"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/repository/postgres"
)

type runtimeState struct {
	config  Config
	source  *postgres.Repository
	target  *postgres.Repository
	restore *postgres.Repository
	server  *observability.Server
}

func Run(lifecycle context.Context, shutdownBase context.Context, _ string) (resultErr error) {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	state := &runtimeState{config: config}
	var serveResult chan error
	defer func() {
		if state.server != nil {
			state.server.SetReady(false)
			shutdown, cancel := context.WithTimeout(context.WithoutCancel(shutdownBase), config.ShutdownTimeout)
			defer cancel()
			resultErr = errors.Join(resultErr, state.server.Shutdown(shutdown))
			if serveResult != nil {
				joinTimer := time.NewTimer(config.ShutdownTimeout)
				defer joinTimer.Stop()
				select {
				case serveErr := <-serveResult:
					resultErr = errors.Join(resultErr, serveErr)
				case <-joinTimer.C:
					resultErr = errors.Join(resultErr, errors.New("technical server shutdown join timed out"))
				}
			}
		}
		if state.target != nil {
			state.target.Close()
		}
		if state.restore != nil {
			state.restore.Close()
		}
		if state.source != nil {
			state.source.Close()
		}
	}()
	startup, cancelStartup := context.WithTimeout(lifecycle, config.StartupTimeout)
	defer cancelStartup()
	sourceDSN, err := readDSN(config.SourceDSNFile)
	if err != nil {
		return err
	}
	targetDSN, err := readDSN(config.TargetDSNFile)
	if err != nil {
		return err
	}
	state.source, err = postgres.Open(startup, postgres.ConnectionConfig{
		DSN: sourceDSN, TLSServerName: config.SourceTLSServerName, CAFile: config.SourceCAFile,
		RequiredRole: "matter_codex_migration",
	})
	if err != nil {
		return err
	}
	state.target, err = postgres.Open(startup, postgres.ConnectionConfig{
		DSN: targetDSN, TLSServerName: config.TargetTLSServerName, CAFile: config.TargetCAFile,
		RequiredRole: "control_plane_migration",
	})
	if err != nil {
		return err
	}
	state.server, err = observability.Start(config.TechnicalListen, config.Mode)
	if err != nil {
		return err
	}
	serveResult = make(chan error, 1)
	go func() { serveResult <- state.server.Serve() }()
	state.server.SetReady(true)
	operation, cancelOperation := context.WithTimeout(lifecycle, config.OperationTimeout)
	defer cancelOperation()
	runResult := make(chan error, 1)
	go func() { runResult <- state.execute(operation, sourceDSN) }()
	select {
	case err := <-serveResult:
		serveResult = nil
		cancelOperation()
		if joinErr := waitResult(runResult, config.ShutdownTimeout, "migration operation shutdown join timed out"); joinErr != nil {
			return errors.Join(err, joinErr)
		}
		state.server.SetOutcome("error")
		if err == nil {
			return errors.New("technical server stopped unexpectedly")
		}
		return fmt.Errorf("serve technical endpoint: %w", err)
	case err := <-runResult:
		if err != nil {
			state.server.SetOutcome("blocked")
			return err
		}
		state.server.SetOutcome("success")
		return nil
	case <-lifecycle.Done():
		cancelOperation()
		joinErr := waitResult(runResult, config.ShutdownTimeout, "migration operation shutdown join timed out")
		state.server.SetOutcome("error")
		return errors.Join(lifecycle.Err(), joinErr)
	}
}

func waitResult(result <-chan error, timeout time.Duration, timeoutMessage string) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return errors.New(timeoutMessage)
	}
}

func (state *runtimeState) execute(ctx context.Context, sourceDSN string) error {
	switch state.config.Mode {
	case "dry-run":
		plan, err := state.buildPlan(ctx, false)
		if err != nil {
			return err
		}
		if err := writeReport(state.config.ReportPath, plan); err != nil {
			return err
		}
		if !plan.Ready() {
			return errors.New("migration plan is blocked by integrity violations")
		}
		return nil
	case "pre-commit":
		return state.prepare(ctx, sourceDSN)
	case "commit":
		return state.commit(ctx)
	case "rollback":
		return state.rollback(ctx)
	case "restore-verify":
		return state.restoreVerify(ctx)
	default:
		return errors.New("migration mode is invalid")
	}
}

func (state *runtimeState) restoreVerify(ctx context.Context) error {
	sourceReceipt, err := state.source.GetSourceReceipt(ctx, state.config.PlanID)
	if err != nil || sourceReceipt.State != "PREPARED" {
		return errors.New("restore verification requires a prepared source receipt")
	}
	targetReceipt, err := state.target.GetTargetReceipt(ctx, state.config.PlanID)
	if err != nil || targetReceipt.State != "PREPARED" || !sameReceipt(sourceReceipt, targetReceipt) {
		return errors.New("restore verification requires matching prepared receipts")
	}
	restoreDSN, err := readDSN(state.config.RestoreDSNFile)
	if err != nil {
		return err
	}
	state.restore, err = postgres.Open(ctx, postgres.ConnectionConfig{
		DSN: restoreDSN, TLSServerName: state.config.RestoreTLSServerName, CAFile: state.config.RestoreCAFile,
	})
	if err != nil {
		return err
	}
	if err := state.restore.VerifyEmptyRestoreTarget(ctx); err != nil {
		return err
	}
	result, manifest, err := backup.LoadExisting(ctx, state.config.BackupDirectory,
		state.config.PlanID, state.config.BackupKeyFile)
	if err != nil {
		return err
	}
	if result.BackupSHA256 != sourceReceipt.BackupSHA256 || result.ManifestSHA256 != sourceReceipt.ManifestSHA256 ||
		manifest.SourceSHA256 != sourceReceipt.SourceSHA256 {
		return errors.New("backup evidence does not match prepared cutover receipts")
	}
	if err := backup.Restore(ctx, result.BackupPath, state.config.BackupKeyFile, restoreDSN,
		state.config.RestoreTLSServerName, state.config.RestoreCAFile); err != nil {
		return err
	}
	snapshot, err := state.restore.BeginSourceSnapshot(ctx, false, false)
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Tx.Rollback(ctx) }()
	digest, counts := snapshot.SourceSHA256, snapshot.Counts
	if digest != manifest.SourceSHA256 || !sameCounts(counts, manifest.TableCounts) {
		return errors.New("restored source snapshot does not match backup manifest")
	}
	if err := snapshot.Tx.Commit(ctx); err != nil {
		return errors.New("close restored verification snapshot")
	}
	if err := state.target.MarkTargetRestoreVerified(ctx, targetReceipt); err != nil {
		return err
	}
	return writeJSONReport(state.config.ReportPath, model.RestoreVerification{
		SchemaVersion: "mattercodex.legacy-data-restore-verification.v1", PlanID: state.config.PlanID,
		SourceSHA256: digest, BackupSHA256: result.BackupSHA256,
		ManifestSHA256: result.ManifestSHA256, TableCounts: counts,
		Outcome: "verified", VerifiedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
}

func sameCounts(left, right map[string]uint64) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func (state *runtimeState) buildPlan(ctx context.Context, export bool) (model.Plan, error) {
	snapshot, err := state.source.BeginSourceSnapshot(ctx, export, false)
	if err != nil {
		return model.Plan{}, err
	}
	defer func() { _ = snapshot.Tx.Rollback(ctx) }()
	resources, err := state.target.TargetResources(ctx)
	if err != nil {
		return model.Plan{}, err
	}
	plan, err := planner.BuildInventory(state.config.PlanID, snapshot.Rows, snapshot.SourceSHA256,
		snapshot.Counts, resources)
	if err != nil {
		return model.Plan{}, err
	}
	if err := snapshot.Tx.Commit(ctx); err != nil {
		return model.Plan{}, errors.New("close source snapshot")
	}
	return plan, nil
}

func (state *runtimeState) prepare(ctx context.Context, sourceDSN string) error {
	snapshot, err := state.source.BeginSourceSnapshot(ctx, true, false)
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Tx.Rollback(ctx) }()
	resources, err := state.target.TargetResources(ctx)
	if err != nil {
		return err
	}
	plan, err := planner.BuildInventory(state.config.PlanID, snapshot.Rows, snapshot.SourceSHA256,
		snapshot.Counts, resources)
	if err != nil {
		return err
	}
	if !plan.Ready() {
		_ = writeReport(state.config.ReportPath, plan)
		return errors.New("migration plan is blocked by integrity violations")
	}
	backupResult, err := backup.Create(ctx, state.config.BackupDirectory, state.config.PlanID, sourceDSN,
		state.config.SourceTLSServerName, state.config.SourceCAFile, snapshot.ExportedID,
		state.config.BackupKeyFile, plan.SourceSHA256, plan.Counts.Source, time.Now())
	if err != nil {
		return err
	}
	if err := snapshot.Tx.Commit(ctx); err != nil {
		return errors.New("close backed-up source snapshot")
	}
	plan.BackupSHA256 = backupResult.BackupSHA256
	plan.ManifestSHA256 = backupResult.ManifestSHA256
	receipt := receiptFromPlan(plan)
	if err := state.target.PrepareTarget(ctx, receipt, plan.Counts); err != nil {
		return err
	}
	if err := state.source.PrepareSource(ctx, receipt); err != nil {
		return err
	}
	plan.CutoverState = "PREPARED"
	return writeReport(state.config.ReportPath, plan)
}

func (state *runtimeState) commit(ctx context.Context) error {
	sourceReceipt, err := state.source.GetSourceReceipt(ctx, state.config.PlanID)
	if err != nil {
		return errors.New("read prepared source receipt")
	}
	targetReceipt, err := state.target.GetTargetReceipt(ctx, state.config.PlanID)
	if err != nil || !sameReceipt(sourceReceipt, targetReceipt) || !targetReceipt.RestoreVerified ||
		!commitStateAllowed(sourceReceipt.State, targetReceipt.State) {
		return errors.New("prepared cutover receipts mismatch")
	}
	backupEvidence, manifest, err := backup.LoadExisting(ctx, state.config.BackupDirectory,
		state.config.PlanID, state.config.BackupKeyFile)
	if err != nil || backupEvidence.BackupSHA256 != sourceReceipt.BackupSHA256 ||
		backupEvidence.ManifestSHA256 != sourceReceipt.ManifestSHA256 || manifest.SourceSHA256 != sourceReceipt.SourceSHA256 {
		return errors.New("immutable backup evidence does not match cutover receipts")
	}
	sourceSnapshot, err := state.source.BeginSourceSnapshot(ctx, false, true)
	if err != nil {
		return err
	}
	defer func() { _ = sourceSnapshot.Tx.Rollback(ctx) }()
	targetSnapshot, err := state.target.BeginTargetSnapshot(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = targetSnapshot.Tx.Rollback(ctx) }()
	plan, err := planner.BuildInventory(state.config.PlanID, sourceSnapshot.Rows, sourceSnapshot.SourceSHA256,
		sourceSnapshot.Counts, targetSnapshot.Resources)
	if err != nil || !plan.Ready() || plan.PlanSHA256 != sourceReceipt.PlanSHA256 ||
		plan.SourceSHA256 != sourceReceipt.SourceSHA256 || plan.TargetSHA256 != sourceReceipt.TargetSHA256 ||
		!sameCounts(plan.Counts.Source, manifest.TableCounts) {
		return errors.New("concurrent drift blocks migration commit")
	}
	plan.BackupSHA256, plan.ManifestSHA256 = sourceReceipt.BackupSHA256, sourceReceipt.ManifestSHA256
	if err := postgres.FreezeSourceSnapshot(ctx, sourceSnapshot.Tx, sourceReceipt); err != nil {
		return err
	}
	if err := sourceSnapshot.Tx.Commit(ctx); err != nil {
		return errors.New("commit source cutover fence")
	}
	if err := postgres.CommitTargetSnapshot(ctx, targetSnapshot.Tx, targetReceipt); err != nil {
		return err
	}
	if err := targetSnapshot.Tx.Commit(ctx); err != nil {
		return errors.New("commit target cutover receipt")
	}
	if err := state.source.CommitSource(ctx, sourceReceipt); err != nil {
		return err
	}
	committedSource, err := state.source.GetSourceReceipt(ctx, state.config.PlanID)
	if err != nil || committedSource.State != "COMMITTED" || !sameReceipt(sourceReceipt, committedSource) {
		return errors.New("source committed receipt readback mismatch")
	}
	committedTarget, err := state.target.GetTargetReceipt(ctx, state.config.PlanID)
	if err != nil || committedTarget.State != "COMMITTED" || !committedTarget.RestoreVerified ||
		!sameReceipt(targetReceipt, committedTarget) {
		return errors.New("target committed receipt readback mismatch")
	}
	plan.CutoverState = "COMMITTED"
	return writeReport(state.config.ReportPath, plan)
}

func (state *runtimeState) rollback(ctx context.Context) error {
	sourceReceipt, sourceExists, err := state.source.FindSourceReceipt(ctx, state.config.PlanID)
	if err != nil {
		return errors.New("read source receipt for rollback")
	}
	targetReceipt, targetExists, err := state.target.FindTargetReceipt(ctx, state.config.PlanID)
	if err != nil {
		return errors.New("read target receipt for rollback")
	}
	if !sourceExists && !targetExists {
		return errors.New("rollback receipt does not exist")
	}
	if sourceExists && targetExists && !sameReceipt(sourceReceipt, targetReceipt) {
		return errors.New("rollback receipts mismatch")
	}
	if (sourceExists && sourceReceipt.State == "COMMITTED") || (targetExists && targetReceipt.State == "COMMITTED") {
		return errors.New("rollback is forbidden after irreversible cutover")
	}
	if targetExists {
		if err := state.target.AbortTarget(ctx, targetReceipt); err != nil {
			return err
		}
	}
	if sourceExists {
		if err := state.source.AbortSource(ctx, sourceReceipt); err != nil {
			return err
		}
	}
	verifiedSource, verifiedSourceExists, err := state.source.FindSourceReceipt(ctx, state.config.PlanID)
	if err != nil || (verifiedSourceExists && verifiedSource.State != "ABORTED") {
		return errors.New("source rollback readback mismatch")
	}
	verifiedTarget, verifiedTargetExists, err := state.target.FindTargetReceipt(ctx, state.config.PlanID)
	if err != nil || (verifiedTargetExists && verifiedTarget.State != "ABORTED") {
		return errors.New("target rollback readback mismatch")
	}
	auditReceipt := targetReceipt
	if sourceExists {
		auditReceipt = sourceReceipt
	}
	sourceState, targetState := "MISSING", "MISSING"
	if verifiedSourceExists {
		sourceState = verifiedSource.State
	}
	if verifiedTargetExists {
		targetState = verifiedTarget.State
	}
	return writeJSONReport(state.config.ReportPath, model.CutoverAudit{
		SchemaVersion: "mattercodex.legacy-data-cutover-audit.v1", PlanID: auditReceipt.PlanID,
		PlanSHA256: auditReceipt.PlanSHA256, SourceSHA256: auditReceipt.SourceSHA256,
		TargetSHA256: auditReceipt.TargetSHA256, BackupSHA256: auditReceipt.BackupSHA256,
		ManifestSHA256: auditReceipt.ManifestSHA256, SourceState: sourceState, TargetState: targetState,
		Outcome: "aborted", RecordedAt: time.Now().UTC().Truncate(time.Microsecond),
	})
}

func receiptFromPlan(plan model.Plan) model.Receipt {
	return model.Receipt{PlanID: plan.PlanID, PlanSHA256: plan.PlanSHA256,
		SourceSHA256: plan.SourceSHA256, TargetSHA256: plan.TargetSHA256,
		BackupSHA256: plan.BackupSHA256, ManifestSHA256: plan.ManifestSHA256}
}

func sameReceipt(left, right model.Receipt) bool {
	return left.PlanID == right.PlanID && left.PlanSHA256 == right.PlanSHA256 &&
		left.SourceSHA256 == right.SourceSHA256 && left.TargetSHA256 == right.TargetSHA256 &&
		left.BackupSHA256 == right.BackupSHA256 && left.ManifestSHA256 == right.ManifestSHA256
}

func commitStateAllowed(sourceState, targetState string) bool {
	return sourceState == "PREPARED" && targetState == "PREPARED" ||
		sourceState == "FROZEN" && (targetState == "PREPARED" || targetState == "COMMITTED") ||
		sourceState == "COMMITTED" && targetState == "COMMITTED"
}

func writeReport(path string, plan model.Plan) error {
	return writeJSONReport(path, plan)
}

func writeJSONReport(path string, report any) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return errors.New("encode migration report")
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return errors.New("create migration report directory")
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("migration report directory permissions are unsafe")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".migration-report-*")
	if err != nil {
		return errors.New("create migration report")
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return errors.New("protect migration report")
	}
	if _, err := temporary.Write(encoded); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("write migration report")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("commit migration report")
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("open migration report directory")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync migration report directory")
	}
	ok = true
	return nil
}
