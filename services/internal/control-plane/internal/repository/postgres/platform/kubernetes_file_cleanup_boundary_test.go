package platform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestKubernetesFileCleanupRejectsWrongPurposeBeforeStorage requires exact public purpose before any pool/store access.
// A zero repository deliberately contains no database or object adapter; this is not an actor/tenant PostgreSQL proof.
// TestKubernetesFileCleanupRejectsWrongPurposeBeforeStorage требует exact public purpose до pool/store access.
// Zero repository не имеет database/object adapter; это не actor/tenant PostgreSQL proof.
func TestKubernetesFileCleanupRejectsWrongPurposeBeforeStorage(t *testing.T) {
	repository := &Repository{}
	p := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", CorrelationRef: "file-boundary", CredentialRevision: 1, CallerWorkload: "integration-gateway", Permission: "platform.integrations.kubernetes.file.get"}
	if _, err := repository.GetKubernetesFileCleanup(t.Context(), p, query.KubernetesFileCleanupRead{InvocationRef: "inv_synthetic", SourceRef: "kfs_synthetic"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("private caller reached public cleanup", err)
	}
	p.CallerWorkload = "control-api-gateway"
	if _, err := repository.ListKubernetesFileCleanup(t.Context(), p, "inv_synthetic"); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("get purpose substituted list purpose", err)
	}
	if _, err := repository.RepairKubernetesFileCleanup(t.Context(), p, value.Mutation{}, command.KubernetesFileCleanupRepair{}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("metadata purpose admitted cleanup mutation", err)
	}
	p.Permission = kubernetesFileCleanupRepairOperation
	if _, err := repository.RepairKubernetesFileCleanup(t.Context(), p, value.Mutation{}, command.KubernetesFileCleanupRepair{}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("missing repair OCC reached database", err)
	}
}

// TestKubernetesFileReservationRejectsExpiredAuthorityBeforeIntent protects the FILE-only insert seam from stale contexts.
// It proves no pool/PUT access on cancellation/expiry, not the real project/mutex/COMMIT ordering.
// TestKubernetesFileReservationRejectsExpiredAuthorityBeforeIntent защищает FILE-only insert от stale contexts.
// Проверяет отсутствие pool/PUT access при cancellation/expiry, не actual project/mutex/COMMIT ordering.
func TestKubernetesFileReservationRejectsExpiredAuthorityBeforeIntent(t *testing.T) {
	repository := &Repository{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.reserveKubernetesFilePrepared(ctx, kubernetesFilePreparedSource{}, preparedContentBinding{}, nil); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("cancelled reservation reached storage", err)
	}
	source := kubernetesFilePreparedSource{ref: "kfs_synthetic", authorizedUntil: time.Now().Add(-time.Second)}
	if _, err := repository.reserveKubernetesFilePrepared(t.Context(), source, preparedContentBinding{}, nil); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("expired FILE allowance became malformed binding", err)
	}
	source.authorizedUntil = time.Now().Add(time.Minute)
	if _, err := repository.reserveKubernetesFilePrepared(t.Context(), source, preparedContentBinding{SourceProfile: "KUBERNETES_OUTPUT"}, nil); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("foreign profile reached FILE reservation mutex", err)
	}
}
