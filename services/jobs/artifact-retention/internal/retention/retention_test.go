package retention

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// fakeRepository returns fixed claims and records finalization order and exact claims.
// RU: fakeRepository возвращает заданные claims и фиксирует порядок и точные claims финализации.
type fakeRepository struct {
	claims    []Claim
	finalized []Claim
	events    *[]string
}

// Claim returns the synthetic due versions without changing their ownership state.
// RU: Claim возвращает синтетические версии к очистке, не меняя их состояние владения.
func (repository *fakeRepository) Claim(context.Context, string, int, int64) ([]Claim, error) {
	return repository.claims, nil
}

// Finalize records a terminal repository action after the test's object readback.
// RU: Finalize фиксирует конечное действие repository после проверки объекта в тесте.
func (repository *fakeRepository) Finalize(_ context.Context, claim Claim, _ string) error {
	if repository.events != nil {
		*repository.events = append(*repository.events, "finalize")
	}
	repository.finalized = append(repository.finalized, claim)
	return nil
}

// fakeObjects captures exact delete/head arguments and injects object-store outcomes.
// RU: fakeObjects сохраняет точные аргументы delete/head и подставляет исходы объектного хранилища.
type fakeObjects struct {
	deleteKey, deleteVersion string
	headKey, headVersion     string
	deleteErr                error
	headErr                  error
	events                   *[]string
}

// Delete records the target version and returns the configured deletion outcome.
// RU: Delete записывает целевую версию и возвращает заданный исход удаления.
func (objects *fakeObjects) Delete(_ context.Context, key, version string) error {
	if objects.events != nil {
		*objects.events = append(*objects.events, "delete")
	}
	objects.deleteKey, objects.deleteVersion = key, version
	return objects.deleteErr
}

// Head records the target version and returns the configured readback outcome.
// RU: Head записывает целевую версию и возвращает заданный исход контрольного чтения.
func (objects *fakeObjects) Head(_ context.Context, key, version string) (objectstorage.Receipt, error) {
	if objects.events != nil {
		*objects.events = append(*objects.events, "head")
	}
	objects.headKey, objects.headVersion = key, version
	return objectstorage.Receipt{Key: key, VersionID: version}, objects.headErr
}

// TestProcessDeletesExactVersionBeforeTombstone asserts delete, absent readback and
// repository finalization occur in that order for the same claimed object version.
// RU: TestProcessDeletesExactVersionBeforeTombstone проверяет порядок удаления,
// подтверждения отсутствия и финализации одной заявленной версии объекта.
func TestProcessDeletesExactVersionBeforeTombstone(t *testing.T) {
	events := make([]string, 0, 3)
	repository := &fakeRepository{
		claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", RevisionID: "revision_id", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}},
		events: &events,
	}
	objects := &fakeObjects{headErr: objectstorage.ErrNotFound, events: &events}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if processed != 1 || objects.deleteKey != "org/file" || objects.deleteVersion != "v7" || objects.headKey != "org/file" || objects.headVersion != "v7" {
		t.Fatalf("unexpected exact deletion/readback: processed=%d delete=%q@%q head=%q@%q", processed, objects.deleteKey, objects.deleteVersion, objects.headKey, objects.headVersion)
	}
	if len(repository.finalized) != 1 || repository.finalized[0].Generation != 3 {
		t.Fatalf("unexpected finalization: %#v", repository.finalized)
	}
	if got, want := fmt.Sprint(events), "[delete head finalize]"; got != want {
		t.Fatalf("unexpected lifecycle order: got %s want %s", got, want)
	}
}

// TestProcessDoesNotTombstoneFailedDeletion keeps the claim open when object deletion fails.
// RU: TestProcessDoesNotTombstoneFailedDeletion не завершает claim при отказе удаления объекта.
func TestProcessDoesNotTombstoneFailedDeletion(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", RevisionID: "revision_id", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{deleteErr: errors.New("object storage unavailable")}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("failed deletion was finalized: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

// TestProcessFinalizesAlreadyMissingExactVersion proves retry is idempotent after prior deletion.
// RU: TestProcessFinalizesAlreadyMissingExactVersion доказывает идемпотентность повтора после удаления.
func TestProcessFinalizesAlreadyMissingExactVersion(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", RevisionID: "revision_id", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{deleteErr: objectstorage.ErrNotFound, headErr: objectstorage.ErrNotFound}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err != nil || processed != 1 || len(repository.finalized) != 1 {
		t.Fatalf("idempotent retry failed: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

// TestProcessDoesNotTombstoneObjectStillPresentAfterDelete rejects a non-effective delete.
// RU: TestProcessDoesNotTombstoneObjectStillPresentAfterDelete отклоняет удаление без фактического эффекта.
func TestProcessDoesNotTombstoneObjectStillPresentAfterDelete(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", RevisionID: "revision_id", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("live object version was tombstoned: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

// TestProcessDoesNotTombstoneUncertainHeadFailure refuses finalization on readback outage.
// RU: TestProcessDoesNotTombstoneUncertainHeadFailure запрещает финализацию при сбое контрольного чтения.
func TestProcessDoesNotTombstoneUncertainHeadFailure(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", RevisionID: "revision_id", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{headErr: objectstorage.ErrUnavailable}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || !errors.Is(err, objectstorage.ErrUnavailable) || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("uncertain readback was tombstoned: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}
