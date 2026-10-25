package s3backup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// inspectionNoWrites fails every mutation so a conflict or an unchanged version cannot hide an attempted external write.
// inspectionNoWrites отклоняет каждую мутацию: конфликт либо прежняя версия не могут скрыть попытку внешней записи.
type inspectionNoWrites struct {
	*fakeS3
	writes     int
	headStatus int
	getStatus  int
}

// HeadObject injects exact-version metadata failures after a successful inventory, without changing stored versions.
// HeadObject вводит ошибку метаданных точной версии после успешного inventory без изменения сохранённых версий.
func (store *inspectionNoWrites) HeadObject(ctx context.Context, input *s3.HeadObjectInput, options ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if store.headStatus != 0 {
		return nil, responseError(store.headStatus)
	}
	return store.fakeS3.HeadObject(ctx, input, options...)
}

// GetObject injects exact-version content failures after metadata resolution, preserving read-only fixture semantics.
// GetObject вводит ошибку содержимого точной версии после получения метаданных, сохраняя чтение без записи.
func (store *inspectionNoWrites) GetObject(ctx context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if store.getStatus != 0 {
		return nil, responseError(store.getStatus)
	}
	return store.fakeS3.GetObject(ctx, input, options...)
}

// PutObject records a forbidden write without changing fixture storage.
// PutObject учитывает запрещённую запись без изменения тестового хранилища.
func (store *inspectionNoWrites) PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	store.writes++
	return nil, errors.New("inspection attempted a write")
}

// TestInspectOperationReadFailuresNeverBecomeAbsence rejects vanished or inaccessible listed versions instead of reporting a safe-to-repeat operation.
// TestInspectOperationReadFailuresNeverBecomeAbsence отклоняет исчезнувшие либо недоступные найденные версии вместо сообщения об отсутствии блокировки.
func TestInspectOperationReadFailuresNeverBecomeAbsence(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
		for _, phase := range []string{"head", "get"} {
			repository, fake := testRepository(t)
			now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			if _, err := repository.AcquireOperationLock(t.Context(), "verify", "private-operation-canary", now, 10*time.Minute); err != nil {
				t.Fatal(err)
			}
			guard := &inspectionNoWrites{fakeS3: fake}
			if phase == "head" {
				guard.headStatus = status
			} else {
				guard.getStatus = status
			}
			repository.destination.api = guard
			result, err := repository.InspectOperation(t.Context(), now)
			if err == nil || result.State != "" || guard.writes != 0 {
				t.Fatalf("listed version %s failure %d became an observation or write", phase, status)
			}
			if status == http.StatusServiceUnavailable && !strings.Contains(err.Error(), "S3 operation failed: server") {
				t.Fatalf("listed version %s failure lost the closed S3 class", phase)
			}
		}
	}
}

// DeleteObject records a forbidden delete without removing a fixture version.
// DeleteObject учитывает запрещённое удаление без удаления тестовой версии.
func (store *inspectionNoWrites) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	store.writes++
	return nil, errors.New("inspection attempted a delete")
}

// TestInspectOperationNeverMutates distinguishes absent, active, expired and corrupt locks while retaining all existing versions and hiding operation identifiers.
// TestInspectOperationNeverMutates различает отсутствующую, активную, просроченную и повреждённую блокировки, сохраняя версии и скрывая идентификаторы операций.
func TestInspectOperationNeverMutates(t *testing.T) {
	for _, state := range []string{"ABSENT", "ACTIVE", "EXPIRED", "CORRUPT"} {
		t.Run(state, func(t *testing.T) {
			repository, fake := testRepository(t)
			now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			if state != "ABSENT" {
				document := operationLockDocument{SchemaVersion: 1, Kind: "kodex-backup-operation-lock", Operation: "verify", OperationID: "private-operation-canary", AcquiredAt: now, ExpiresAt: now.Add(10 * time.Minute)}
				if state == "CORRUPT" {
					document.Operation = "private-operation-canary"
				}
				if _, err := repository.PutJSON(t.Context(), "locks/controller.json", document); err != nil {
					t.Fatal(err)
				}
			}
			if state == "EXPIRED" {
				now = now.Add(10 * time.Minute)
			}
			guard := &inspectionNoWrites{fakeS3: fake}
			repository.destination.api = guard
			version, count := fake.nextVersion, len(fake.objects)
			result, err := repository.InspectOperation(t.Context(), now)
			if state == "CORRUPT" {
				if err == nil || result.State != "" {
					t.Fatal("corrupt lock became a valid observation")
				}
			} else if err != nil || result.State != state {
				t.Fatal("lock state was not observed exactly", err)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "private-operation-canary") || guard.writes != 0 || fake.nextVersion != version || len(fake.objects) != count {
				t.Fatal("inspection leaked an identifier or mutated storage")
			}
			if state == "ACTIVE" || state == "EXPIRED" {
				if len(result.SnapshotSHA256) != 71 || result.Operation != "verify" {
					t.Fatal("missing exact lock binding")
				}
			}
		})
	}
}
