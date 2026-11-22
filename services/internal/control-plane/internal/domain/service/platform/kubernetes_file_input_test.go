package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// fileOwnerFixture observes protected forwarding and compatibility without claiming storage or native effects.
// fileOwnerFixture наблюдает protected forwarding и compatibility без выдуманной storage либо native effects.
type fileOwnerFixture struct {
	*managedKubernetesRepositoryFixture
	fileRevision uint32
	fileCalls    int
}

// ListKubernetesFileInputs counts metadata discovery, retaining no payload or execution credit.
// ListKubernetesFileInputs учитывает metadata discovery без payload либо execution credit.
func (f *fileOwnerFixture) ListKubernetesFileInputs(context.Context, value.Principal, string) ([]entity.KubernetesFileInputView, error) {
	f.fileCalls++
	return nil, nil
}

// GetKubernetesFileInput counts one metadata read without preparing an artifact.
// GetKubernetesFileInput учитывает metadata read без подготовки artifact.
func (f *fileOwnerFixture) GetKubernetesFileInput(context.Context, value.Principal, string, string) (entity.KubernetesFileInputView, error) {
	f.fileCalls++
	return entity.KubernetesFileInputView{}, nil
}

// PrepareManagedKubernetesFileInput observes only the validated broker route, not durable preparation.
// PrepareManagedKubernetesFileInput наблюдает validated broker route, не durable preparation.
func (f *fileOwnerFixture) PrepareManagedKubernetesFileInput(context.Context, value.Principal, value.ManagedKubernetesBinding, string) (entity.ManagedKubernetesFilePreparation, error) {
	f.fileCalls++
	return entity.ManagedKubernetesFilePreparation{}, nil
}

// ManagedKubernetesFileInputContract supplies fixture compatibility independently of generic delivery support.
// ManagedKubernetesFileInputContract задаёт fixture compatibility независимо от generic delivery support.
func (f *fileOwnerFixture) ManagedKubernetesFileInputContract(context.Context, value.Principal) (uint32, error) {
	return f.fileRevision, nil
}

// fileCleanupOwnerFixture adds the optional closure interface without claiming a durable worker or deletion proof.
// fileCleanupOwnerFixture добавляет optional closure interface без выдуманного durable worker либо deletion proof.
type fileCleanupOwnerFixture struct {
	*fileOwnerFixture
	cleanupCalls int
}

// ListKubernetesFileCleanup counts minimal metadata forwarding without source-byte reads.
// ListKubernetesFileCleanup учитывает minimal metadata forwarding без source-byte reads.
func (f *fileCleanupOwnerFixture) ListKubernetesFileCleanup(context.Context, value.Principal, string) ([]entity.KubernetesFileCleanupView, error) {
	f.cleanupCalls++
	return nil, nil
}

// GetKubernetesFileCleanup records readback forwarding without reissuing repair.
// GetKubernetesFileCleanup фиксирует readback forwarding без повторного repair.
func (f *fileCleanupOwnerFixture) GetKubernetesFileCleanup(context.Context, value.Principal, query.KubernetesFileCleanupRead) (entity.KubernetesFileCleanupReadback, error) {
	f.cleanupCalls++
	return entity.KubernetesFileCleanupReadback{}, nil
}

// RepairKubernetesFileCleanup observes typed closure forwarding, never a storage/native operation.
// RepairKubernetesFileCleanup наблюдает typed closure forwarding, не storage/native operation.
func (f *fileCleanupOwnerFixture) RepairKubernetesFileCleanup(context.Context, value.Principal, value.Mutation, command.KubernetesFileCleanupRepair) (entity.KubernetesFileCleanupReadback, error) {
	f.cleanupCalls++
	return entity.KubernetesFileCleanupReadback{}, nil
}

// TestKubernetesFileInputPurposeBoundary rejects wrong callers, public claim substitutes and absent ports before forwarding.
// TestKubernetesFileInputPurposeBoundary отклоняет wrong callers, public claim substitutes и absent ports до forwarding.
func TestKubernetesFileInputPurposeBoundary(t *testing.T) {
	service, base, p, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.file.prepare")
	if _, err := service.PrepareManagedKubernetesFileInput(t.Context(), p, binding, "kch_synthetic"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing FILE owner became success", err)
	}
	f := &fileOwnerFixture{managedKubernetesRepositoryFixture: base}
	service, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []value.Principal{
		{CallerWorkload: "integration-gateway", Permission: p.Permission},
		{CallerWorkload: "control-api-gateway", Permission: "platform.integrations.kubernetes.file.get"},
	} {
		wrong.ActorID = p.ActorID
		wrong.AuthorityTenant = p.AuthorityTenant
		wrong.CorrelationRef = p.CorrelationRef
		wrong.CredentialRevision = p.CredentialRevision
		if _, err := service.PrepareManagedKubernetesFileInput(t.Context(), wrong, binding, "kch_synthetic"); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("wrong FILE purpose admitted", err)
		}
	}
	if f.fileCalls != 0 {
		t.Fatal("wrong purpose reached owner")
	}
	if _, err := service.PrepareManagedKubernetesFileInput(t.Context(), p, binding, "kch_synthetic"); err != nil || f.fileCalls != 1 {
		t.Fatal("protected FILE purpose rejected", err)
	}
	p.CallerWorkload = "control-api-gateway"
	p.Permission = "platform.integrations.kubernetes.file.list"
	if _, err := service.ListKubernetesFileInputs(t.Context(), p, binding.InvocationRef); err != nil || f.fileCalls != 2 {
		t.Fatal("metadata discovery route rejected", err)
	}
	p.Permission = "platform.integrations.kubernetes.file.get"
	if _, err := service.GetKubernetesFileInput(t.Context(), p, binding.InvocationRef, "kfs_synthetic"); err != nil || f.fileCalls != 3 {
		t.Fatal("metadata read route rejected", err)
	}
	if _, err := service.GetKubernetesFileInput(t.Context(), p, binding.InvocationRef, "../foreign"); !errors.Is(err, errs.ErrInvalid) || f.fileCalls != 3 {
		t.Fatal("invalid locator reached owner", err)
	}
}

// TestKubernetesFileInputReadinessDoesNotInventPorts closes absent and future compatibility instead of borrowing OUTPUT or health.
// TestKubernetesFileInputReadinessDoesNotInventPorts закрывает absent/future compatibility без подмены OUTPUT либо health.
func TestKubernetesFileInputReadinessDoesNotInventPorts(t *testing.T) {
	service, base, p, _ := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.readiness")
	if revision, err := service.CheckManagedKubernetesFileInputContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("missing FILE port invented readiness", err)
	}
	if revision, err := service.CheckManagedKubernetesInputContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("missing INPUT port invented readiness", err)
	}
	f := &fileOwnerFixture{managedKubernetesRepositoryFixture: base, fileRevision: managedkubernetes.FileInputContractRevision}
	service, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	if revision, err := service.CheckManagedKubernetesFileInputContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("missing cleanup port invented full FILE readiness", err)
	}
	service, err = New(&fileCleanupOwnerFixture{fileOwnerFixture: f})
	if err != nil {
		t.Fatal(err)
	}
	if revision, err := service.CheckManagedKubernetesFileInputContract(t.Context(), p); err != nil || revision != managedkubernetes.FileInputContractRevision {
		t.Fatal("known FILE revision rejected", err)
	}
	f.fileRevision++
	if _, err := service.CheckManagedKubernetesFileInputContract(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("future FILE revision downgraded", err)
	}
	p.CallerWorkload = "integration-gateway"
	if _, err := service.CheckManagedKubernetesFileInputContract(t.Context(), p); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("different workload gained FILE readiness", err)
	}
}

// TestKubernetesFileCleanupPurposeBoundary keeps closure-only metadata separate from source visibility and mutation permission.
// TestKubernetesFileCleanupPurposeBoundary отделяет closure-only metadata от source visibility и mutation permission.
func TestKubernetesFileCleanupPurposeBoundary(t *testing.T) {
	service, base, p, binding := managedKubernetesServiceFixture(t, "platform.integrations.kubernetes.file.get")
	p.CallerWorkload = "control-api-gateway"
	input := query.KubernetesFileCleanupRead{InvocationRef: binding.InvocationRef, SourceRef: "kfs_synthetic", CleanupIdempotencyKey: "original-cleanup-key"}
	if _, err := service.GetKubernetesFileCleanup(t.Context(), p, input); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing cleanup port invented metadata", err)
	}
	f := &fileCleanupOwnerFixture{fileOwnerFixture: &fileOwnerFixture{managedKubernetesRepositoryFixture: base}}
	service, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetKubernetesFileCleanup(t.Context(), p, input); err != nil || f.cleanupCalls != 1 || f.fileCalls != 0 {
		t.Fatal("cleanup read borrowed source port", err)
	}
	version := int64(3)
	mutation := value.Mutation{IdempotencyKey: "original-cleanup-key", ExpectedVersion: &version}
	commandInput := command.KubernetesFileCleanupRepair{InvocationRef: input.InvocationRef, SourceRef: input.SourceRef}
	if _, err := service.RepairKubernetesFileCleanup(t.Context(), p, mutation, commandInput); !errors.Is(err, errs.ErrForbidden) || f.cleanupCalls != 1 {
		t.Fatal("read purpose admitted cleanup write", err)
	}
	p.Permission = "platform.integrations.kubernetes.file.cleanup.repair"
	if _, err := service.RepairKubernetesFileCleanup(t.Context(), p, mutation, commandInput); err != nil || f.cleanupCalls != 2 || f.fileCalls != 0 {
		t.Fatal("typed cleanup write lost original mutation", err)
	}
	p.CallerWorkload = "security-bootstrap"
	if _, err := service.RepairKubernetesFileCleanup(t.Context(), p, mutation, commandInput); !errors.Is(err, errs.ErrForbidden) || f.cleanupCalls != 2 {
		t.Fatal("different workload gained cleanup authority", err)
	}
}
