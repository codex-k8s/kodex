package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryKubernetesCaptureReadiness verifies the producer, versioned preparation and cleanup graph.
// It deliberately does not require a worker heartbeat before that same worker can start.
// queryKubernetesCaptureReadiness проверяет producer, versioned preparation и cleanup graph.
// Worker heartbeat не требуется до возможности запуска этого worker.
//
//go:embed sql/kubernetes_capture__readiness.sql
var queryKubernetesCaptureReadiness string

// CheckManagedKubernetesCaptureReadiness exercises the job's exact protected producer path and
// read-only storage versioning probe, plus the configured canonical AV mode on an empty body.
// No probe object, fake artifact, native grant or consumer heartbeat is created by this read.
// CheckManagedKubernetesCaptureReadiness проверяет exact protected producer path job,
// read-only storage versioning и configured canonical AV mode на empty body.
// Probe object, fake artifact, native grant либо consumer heartbeat этим read не создаются.
func (repository *Repository) CheckManagedKubernetesCaptureReadiness(ctx context.Context, p value.Principal) (uint32, error) {
	if p.CallerWorkload != "artifact-retention" || p.Permission != "platform.artifacts.kubernetes-capture.readiness" || p.ProjectRef != "" {
		return 0, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, p); err != nil {
		return 0, err
	}
	if repository.kubernetesCaptureSlots == nil || cap(repository.kubernetesCaptureSlots) != 1 || repository.objects == nil || !repository.kubernetesCaptureSpool.available() {
		return 0, nil
	}
	var ready bool
	if err := repository.pool.QueryRow(ctx, queryKubernetesCaptureReadiness).Scan(&ready); err != nil {
		return 0, errs.ErrUnavailable
	}
	if !ready {
		return 0, nil
	}
	versioned, ok := repository.objects.(objectstorage.VersionedChecker)
	if !ok {
		return 0, nil
	}
	if err := versioned.CheckVersioning(ctx); err != nil {
		return 0, errs.ErrUnavailable
	}
	if err := repository.objects.Check(ctx); err != nil {
		return 0, errs.ErrUnavailable
	}
	empty := sha256.Sum256(nil)
	if _, err := repository.scanArtifact(ctx, bytes.NewReader(nil), "sha256:"+hex.EncodeToString(empty[:]), 0); err != nil {
		return 0, err
	}
	return managedkubernetes.CaptureContractRevision, nil
}
