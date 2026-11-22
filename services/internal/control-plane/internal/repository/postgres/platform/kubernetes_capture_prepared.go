package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/opaqueref"
)

// validKubernetesCapturePreparedBinding admits only the fixed owner-produced capture channel
// recipe. Ordinary assistant prepared-content profiles retain their existing one-MiB boundary.
// This syntax check never authorizes a capture; its INSERT trigger rejoins the sealed owner source.
// validKubernetesCapturePreparedBinding допускает только fixed owner-produced capture channel
// recipe. Ordinary assistant prepared-content profiles сохраняют existing one-MiB boundary.
// Syntax check не разрешает capture; INSERT trigger повторно связывает sealed owner source.
func validKubernetesCapturePreparedBinding(binding preparedContentBinding) bool {
	channel := strings.TrimPrefix(binding.OperationKey, "kubernetes.output.")
	return binding.SourceProfile == "KUBERNETES_OUTPUT" && captureOutputChannel(channel) && binding.OperationKey == "kubernetes.output."+channel &&
		binding.IntentOperation == "kubernetes.capture.finalize" && binding.IdempotencyKey == binding.SourceProfileRef+":"+channel &&
		opaqueref.Valid(binding.SourceProfileRef, "koc") && opaqueref.Valid(binding.TargetArtifactRef, "art") &&
		(binding.TargetArtifactID != "" || binding.ExpectedArtifactVersion == 0 && binding.SourceRevisionRef == "")
}

// currentKubernetesCaptureWriter checks a fresh statement after acquiring the prepared row lock.
// It adds no capture/root lock after prepared_content, avoiding inversion with adoption or closure.
// A revoked/expired old attempt cannot dispatch a new PUT under a successor's task grant.
// currentKubernetesCaptureWriter проверяет fresh statement после prepared row lock.
// Capture/root locks после prepared_content не берутся, исключая inversion с adoption/closure.
// Revoked/expired old attempt не начинает новый PUT под successor task grant.
func currentKubernetesCaptureWriter(ctx context.Context, runner queryRunner, task managedkubernetes.CaptureTaskBinding) error {
	args, err := kubernetesCaptureTaskArgs(task)
	if err != nil {
		return err
	}
	var id string
	if err := runner.QueryRow(ctx, queryKubernetesCaptureWriterCurrent, args).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	} else if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// queryKubernetesCaptureWriterCurrent reads only current task eligibility after the writer lock.
// queryKubernetesCaptureWriterCurrent читает current task eligibility после writer lock.
//
//go:embed sql/kubernetes_capture__writer_current.sql
var queryKubernetesCaptureWriterCurrent string
