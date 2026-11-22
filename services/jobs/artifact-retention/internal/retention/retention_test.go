package retention

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
	for index := range repository.claims {
		if repository.claims[index].AuthorizedUntil.IsZero() {
			repository.claims[index].AuthorizedUntil = time.Now().Add(time.Minute)
		}
	}
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

// CheckDeleteOnce attests only the synthetic adapter capability.
// CheckDeleteOnce подтверждает только synthetic adapter capability.
func (objects *fakeObjects) CheckDeleteOnce(context.Context) error { return nil }

// DeleteOnce records the optional exact one-attempt path independently of ordinary scheduled IO.
// DeleteOnce фиксирует optional exact one-attempt path отдельно от ordinary scheduled IO.
func (objects *fakeObjects) DeleteOnce(ctx context.Context, key, version string) error {
	if objects.events != nil {
		*objects.events = append(*objects.events, "delete-once")
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

// TestManualClaimNeverReplaysAnAdmittedDelete distinguishes fresh custody, unknown replay and
// successful absence without substituting the ordinary retrying deletion path.
// TestManualClaimNeverReplaysAnAdmittedDelete различает fresh custody, unknown replay и successful
// absence без подмены обычным retrying deletion path.
func TestManualClaimNeverReplaysAnAdmittedDelete(t *testing.T) {
	for _, test := range []struct {
		name               string
		reconcile          bool
		deleteErr, headErr error
		want               string
		completed          int
	}{
		{"first", false, nil, objectstorage.ErrNotFound, "[delete-once head finalize]", 1},
		{"unknown-first", false, objectstorage.ErrUnavailable, nil, "[delete-once]", 0},
		{"unknown-still-present", true, nil, nil, "[head]", 0},
		{"unknown-absent-unqualified", true, nil, objectstorage.ErrNotFound, "[head]", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "art", RevisionID: "revision", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 4, Manual: true, ReconcileOnly: test.reconcile}}, events: &events}
			objects := &fakeObjects{deleteErr: test.deleteErr, headErr: test.headErr, events: &events}
			completed, err := NewProcessor(repository, objects).Process(t.Context(), "owner", 1, 60)
			if completed != test.completed || fmt.Sprint(events) != test.want || (err == nil) != (test.completed == 1) {
				t.Fatalf("unexpected manual custody: events=%v completed=%d failed=%t", events, completed, err != nil)
			}
		})
	}
}

// TestExpiredManualAdmissionNeverCallsStorage proves delayed claim ACK grants no late credit.
// TestExpiredManualAdmissionNeverCallsStorage доказывает отсутствие late credit после delayed claim ACK.
func TestExpiredManualAdmissionNeverCallsStorage(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "art", RevisionID: "revision", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 4, Manual: true, AuthorizedUntil: time.Now().Add(-time.Second)}}, events: &events}
	completed, err := NewProcessor(repository, &fakeObjects{events: &events}).Process(t.Context(), "owner", 1, 60)
	if completed != 0 || !errors.Is(err, ErrLostClaim) || len(events) != 0 {
		t.Fatal("expired custody reached storage")
	}
}

// qualifiedManualObjects exposes only an explicitly qualified local exact-version absence proof.
// RU: qualifiedManualObjects предоставляет лишь явно квалифицированный local exact-version absence proof.
type qualifiedManualObjects struct{ *fakeObjects }

// VerifyVersionAbsent proves the expected immutable fixture coordinates after its simulated deletion.
// RU: VerifyVersionAbsent доказывает ожидаемые immutable fixture coordinates после simulated deletion.
func (objects qualifiedManualObjects) VerifyVersionAbsent(ctx context.Context, key, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key != "org/file" || version != "v7" || !errors.Is(objects.headErr, objectstorage.ErrNotFound) {
		return objectstorage.ErrConflict
	}
	return nil
}

// TestManualQualifiedAbsenceAllowsHeadOnlyRecovery keeps Delete disabled while finalizing proven absence.
// RU: TestManualQualifiedAbsenceAllowsHeadOnlyRecovery завершает доказанное отсутствие без нового Delete.
func TestManualQualifiedAbsenceAllowsHeadOnlyRecovery(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{claims: []Claim{{ArtifactID: "id", ArtifactRef: "art", RevisionID: "revision", ObjectKey: "org/file", ObjectVersion: "v7", Generation: 4, Manual: true, ReconcileOnly: true}}, events: &events}
	objects := qualifiedManualObjects{fakeObjects: &fakeObjects{headErr: objectstorage.ErrNotFound, events: &events}}
	n, err := NewProcessor(repository, objects).Process(t.Context(), "owner", 1, 60)
	if n != 1 || err != nil || fmt.Sprint(events) != "[head finalize]" {
		t.Fatal("qualified reconciliation did not finalize", err)
	}
}
