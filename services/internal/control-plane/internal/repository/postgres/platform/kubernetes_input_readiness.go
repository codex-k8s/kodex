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

// queryKubernetesInputReadiness checks the actual one-attempt INPUT schema and runtime privileges.
// queryKubernetesInputReadiness проверяет actual one-attempt INPUT schema и runtime privileges.
//
//go:embed sql/kubernetes_input__readiness.sql
var queryKubernetesInputReadiness string

// queryKubernetesFileReadiness requires the typed source/adoption/cleanup graph rather than only a table name.
// queryKubernetesFileReadiness требует typed source/adoption/cleanup graph, не только имя таблицы.
//
//go:embed sql/kubernetes_file__readiness.sql
var queryKubernetesFileReadiness string

// kubernetesInputStorageReady checks the existing exact versioned storage adapter without writing probe bytes.
// No missing DSN, storage capability or optional port becomes a positive compatibility proof.
// kubernetesInputStorageReady проверяет existing exact versioned storage adapter без записи probe bytes.
// Missing DSN, storage capability либо optional port не становится positive compatibility proof.
func (repository *Repository) kubernetesInputStorageReady(ctx context.Context, p value.Principal, query string) (bool, error) {
	if p.CallerWorkload != "secret-broker" || p.Permission != "platform.runtime.kubernetes.readiness" || p.ProjectRef != "" {
		return false, errs.ErrForbidden
	}
	if repository.pool == nil || repository.objects == nil {
		return false, nil
	}
	if _, err := repository.resolveScope(ctx, p); err != nil {
		return false, err
	}
	var ready bool
	if repository.pool.QueryRow(ctx, query).Scan(&ready) != nil {
		return false, errs.ErrUnavailable
	}
	if !ready {
		return false, nil
	}
	versioned, ok := repository.objects.(objectstorage.VersionedChecker)
	if !ok {
		return false, nil
	}
	if err := versioned.CheckVersioning(ctx); err != nil {
		return false, errs.ErrUnavailable
	}
	if err := repository.objects.Check(ctx); err != nil {
		return false, errs.ErrUnavailable
	}
	return true, nil
}

// ManagedKubernetesInputContract reports revision one only for the real registered owner INPUT path.
// Method registration and transport identities are additionally checked by service/client startup.
// ManagedKubernetesInputContract сообщает revision one лишь для real registered owner INPUT path.
// Method registration и transport identities дополнительно проверяет service/client startup.
func (repository *Repository) ManagedKubernetesInputContract(ctx context.Context, p value.Principal) (uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ready, err := repository.kubernetesInputStorageReady(ctx, p, queryKubernetesInputReadiness)
	if err != nil || !ready {
		return 0, err
	}
	return managedkubernetes.InputContractRevision, nil
}

// ManagedKubernetesFileInputContract requires real Range, canonical AV and the integrated cleanup source.
// It never materializes a fake source or asserts target copy permission from identity health alone.
// ManagedKubernetesFileInputContract требует real Range, canonical AV и integrated cleanup source.
// Fake source не подготавливает, target copy permission из identity health не выводит.
func (repository *Repository) ManagedKubernetesFileInputContract(ctx context.Context, p value.Principal) (uint32, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ready, err := repository.kubernetesInputStorageReady(ctx, p, queryKubernetesFileReadiness)
	if err != nil || !ready {
		return 0, err
	}
	if _, ok := repository.objects.(objectstorage.RangeReader); !ok {
		return 0, nil
	}
	empty := sha256.Sum256(nil)
	if _, err := repository.scanArtifact(ctx, bytes.NewReader(nil), "sha256:"+hex.EncodeToString(empty[:]), 0); err != nil {
		return 0, err
	}
	return managedkubernetes.FileInputContractRevision, nil
}
