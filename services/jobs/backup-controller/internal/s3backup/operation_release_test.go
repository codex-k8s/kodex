package s3backup

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// uncertainLockStore injects a failed delete and a HEAD failure while leaving the listed lock version intact.
// RU: uncertainLockStore вводит ошибку удаления и HEAD, сохраняя найденную версию блокировки.
type uncertainLockStore struct {
	*fakeS3
	deletes      int
	headStatus   int
	deleteStatus int
}

// DeleteObject records one attempted mutation without deleting any object, optionally returning an uncertain success.
// RU: DeleteObject учитывает попытку мутации без удаления объекта, при необходимости возвращая неподтверждённый успех.
func (store *uncertainLockStore) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	store.deletes++
	if store.deleteStatus != 0 {
		return nil, responseError(store.deleteStatus)
	}
	return &s3.DeleteObjectOutput{}, nil
}

// HeadObject hides metadata for an inventoried lock without removing that lock from storage.
// RU: HeadObject скрывает метаданные найденной блокировки, не удаляя её из хранилища.
func (store *uncertainLockStore) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return nil, responseError(store.headStatus)
}

// TestOperationReleaseNeverConfusesHeadFailureWithAbsence rejects false release success for active and expired locks while issuing only one delete per call.
// RU: TestOperationReleaseNeverConfusesHeadFailureWithAbsence отклоняет ложный успех освобождения активной и просроченной блокировки с одной попыткой удаления на вызов.
func TestOperationReleaseNeverConfusesHeadFailureWithAbsence(t *testing.T) {
	for _, deleteStatus := range []int{0, http.StatusServiceUnavailable} {
		for _, headStatus := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable} {
			for _, expired := range []bool{false, true} {
				repository, fake := testRepository(t)
				lock, err := repository.AcquireOperationLock(t.Context(), "verify", "release-test",
					time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				guard := &uncertainLockStore{fakeS3: fake, headStatus: headStatus, deleteStatus: deleteStatus}
				repository.destination.api = guard
				if expired {
					err = repository.deleteExpiredOperationLock(t.Context(), lock.receipt)
				} else {
					err = lock.Release(t.Context())
				}
				if err == nil {
					t.Fatalf("false release success: delete=%d head=%d expired=%t", deleteStatus, headStatus, expired)
				}
				if guard.deletes != 1 || len(fake.objects) != 1 {
					t.Fatal("unexpected deletion or retry")
				}
				repository.destination.api = fake
				state, readErr := repository.InspectOperation(t.Context(), time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC))
				if readErr != nil || state.State != "ACTIVE" {
					t.Fatal("fault fixture did not retain an observable active lock")
				}
			}
		}
	}
}
