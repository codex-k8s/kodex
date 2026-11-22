package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// TestManualPurgeAccessPreservesInfrastructure drives the actual allowance query through failed
// subject resolution. An expired first ceiling never turns failed evidence into NotFound.
// TestManualPurgeAccessPreservesInfrastructure вызывает actual allowance query через failed
// subject resolution. Истёкший first ceiling не превращает failed evidence в NotFound.
func TestManualPurgeAccessPreservesInfrastructure(t *testing.T) {
	for _, item := range []struct {
		name string
		rows []error
		want error
	}{
		{"scan", []error{errors.New("synthetic subject scan failure")}, errs.ErrUnavailable},
		{"joined", []error{errors.Join(pgx.ErrNoRows, errs.ErrUnavailable)}, errs.ErrUnavailable},
		{"missing", []error{pgx.ErrNoRows, pgx.ErrNoRows}, errs.ErrNotFound},
	} {
		t.Run(item.name, func(t *testing.T) {
			tx := &accessFailureTx{rows: item.rows}
			target := resolvedAccessTarget{scope: entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "ARTIFACT"}}
			until, err := (&Repository{}).recheckManualPurgeAccess(t.Context(), tx, scope{organizationID: "org_fixture", actorRef: "sub_fixture"}, target, time.Now().Add(-time.Second))
			if !errors.Is(err, item.want) || !until.IsZero() || tx.calls != len(item.rows) {
				t.Fatal("manual purge concealed infrastructure or granted an allowance")
			}
		})
	}
}

// unqualifiedManualObjects hides local proof while exposing the unchanged generic store contract.
// RU: unqualifiedManualObjects скрывает local proof, сохраняя generic store contract.
type unqualifiedManualObjects struct{ objectstorage.Store }

// Head models generic provider errors without an authenticated target absence proof.
// RU: Head моделирует generic provider errors без authenticated target absence proof.
func (objects unqualifiedManualObjects) Head(context.Context, string, string) (objectstorage.Receipt, error) {
	return objectstorage.Receipt{}, objectstorage.ErrNotFound
}

// TestManualPurgeRequiresQualifiedAbsence rejects ambiguous404 yet permits local exact-version recovery.
// RU: TestManualPurgeRequiresQualifiedAbsence запрещает ambiguous404, допуская local exact-version recovery.
func TestManualPurgeRequiresQualifiedAbsence(t *testing.T) {
	store := objectstoragetest.NewVersioned()
	body := []byte("qualified local version")
	digest := sha256.Sum256(body)
	receipt, err := store.Put(t.Context(), objectstorage.PutInput{Key: "local/manual", MediaType: "text/plain", Digest: fmt.Sprintf("sha256:%x", digest), SizeBytes: int64(len(body)), Body: bytes.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
	object := artifactPurgeObject{ObjectKey: receipt.Key, ObjectVersion: receipt.VersionID}
	repository := &Repository{objects: unqualifiedManualObjects{Store: store}}
	if err := repository.reconcileManualArtifactDelete(t.Context(), object, false); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("unqualified404 finalized manual purge", err)
	}
	if err := store.DeleteOnce(t.Context(), receipt.Key, receipt.VersionID); err != nil {
		t.Fatal(err)
	}
	repository.objects = store
	if err := repository.reconcileManualArtifactDelete(t.Context(), object, false); err != nil {
		t.Fatal("qualified local absence could not reconcile", err)
	}
}
