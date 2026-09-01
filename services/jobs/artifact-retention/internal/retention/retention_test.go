package retention

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

type fakeRepository struct {
	claims    []Claim
	finalized []Claim
	events    *[]string
}

func (repository *fakeRepository) Claim(context.Context, string, int, int64) ([]Claim, error) {
	return repository.claims, nil
}

func (repository *fakeRepository) Finalize(_ context.Context, claim Claim, _ string) error {
	if repository.events != nil {
		*repository.events = append(*repository.events, "finalize")
	}
	repository.finalized = append(repository.finalized, claim)
	return nil
}

type fakeObjects struct {
	deleteKey, deleteVersion string
	headKey, headVersion     string
	deleteErr                error
	headErr                  error
	events                   *[]string
}

func (objects *fakeObjects) Delete(_ context.Context, key, version string) error {
	if objects.events != nil {
		*objects.events = append(*objects.events, "delete")
	}
	objects.deleteKey, objects.deleteVersion = key, version
	return objects.deleteErr
}

func (objects *fakeObjects) Head(_ context.Context, key, version string) (objectstorage.Receipt, error) {
	if objects.events != nil {
		*objects.events = append(*objects.events, "head")
	}
	objects.headKey, objects.headVersion = key, version
	return objectstorage.Receipt{Key: key, VersionID: version}, objects.headErr
}

func TestProcessDeletesExactVersionBeforeTombstone(t *testing.T) {
	events := make([]string, 0, 3)
	repository := &fakeRepository{
		claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}},
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

func TestProcessDoesNotTombstoneFailedDeletion(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{deleteErr: errors.New("object storage unavailable")}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("failed deletion was finalized: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

func TestProcessFinalizesAlreadyMissingExactVersion(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{deleteErr: objectstorage.ErrNotFound, headErr: objectstorage.ErrNotFound}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err != nil || processed != 1 || len(repository.finalized) != 1 {
		t.Fatalf("idempotent retry failed: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

func TestProcessDoesNotTombstoneObjectStillPresentAfterDelete(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("live object version was tombstoned: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}

func TestProcessDoesNotTombstoneUncertainHeadFailure(t *testing.T) {
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "artifact_ref", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 3}}}
	objects := &fakeObjects{headErr: objectstorage.ErrUnavailable}

	processed, err := NewProcessor(repository, objects).Process(context.Background(), "worker-1", 10, 60)
	if err == nil || !errors.Is(err, objectstorage.ErrUnavailable) || processed != 0 || len(repository.finalized) != 0 {
		t.Fatalf("uncertain readback was tombstoned: processed=%d err=%v finalized=%d", processed, err, len(repository.finalized))
	}
}
