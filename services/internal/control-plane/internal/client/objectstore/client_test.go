package objectstore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestInstructionObjectInputGuards(t *testing.T) {
	t.Parallel()

	for _, invalid := range []string{"", "/absolute", "parent/../escape", "line\nbreak", "trailing/"} {
		if !invalidObjectKey(invalid) {
			t.Fatalf("unsafe object key was accepted: %q", invalid)
		}
	}
	if invalidObjectKey("instruction-sets/agent/content.md") {
		t.Fatal("safe object key was rejected")
	}
	if digest([]byte("content")) != "ed7002b439e9ac845f22357d822bac1444730fbdb6016d3ec9432297b9ec9f73" {
		t.Fatal("content digest changed")
	}
	if invalidObjectKey(strings.TrimPrefix(readinessObjectKey, "projects/00000000-0000-0000-0000-000000000000/")) ||
		digest(readinessContent) == "" {
		t.Fatal("readiness canary is outside the bounded production key shape")
	}
	if invalidObjectKey(strings.TrimPrefix(scheduleReadinessKey, "projects/00000000-0000-0000-0000-000000000000/")) ||
		digest(scheduleReadinessContent) == "" {
		t.Fatal("schedule prompt readiness canary is outside the bounded production key shape")
	}
}

type readinessStoreStep struct {
	objects []minio.ObjectInfo
}

type readinessStoreFake struct {
	steps       []readinessStoreStep
	listCall    int
	listOptions []minio.ListObjectsOptions
	removed     []minio.ObjectInfo
	removeError error
}

type serialReadinessFence struct {
	mutex    sync.Mutex
	attempts chan struct{}
}

func (fence *serialReadinessFence) WithInstructionObjectReadinessFence(
	ctx context.Context,
	callback func(context.Context) error,
) error {
	fence.attempts <- struct{}{}
	fence.mutex.Lock()
	defer fence.mutex.Unlock()
	return callback(ctx)
}

func TestReadinessFenceSerializesTwoReplicas(t *testing.T) {
	t.Parallel()

	fence := &serialReadinessFence{attempts: make(chan struct{}, 2)}
	client := &Client{fence: fence}
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondEntered := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		results <- client.withReadinessFence(context.Background(), func(context.Context) error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-fence.attempts
	<-firstEntered
	go func() {
		results <- client.withReadinessFence(context.Background(), func(context.Context) error {
			close(secondEntered)
			return nil
		})
	}()
	// Вторая replica уже запросила fence, но не может начать S3 sequence.
	<-fence.attempts
	select {
	case <-secondEntered:
		t.Fatal("second readiness replica entered before the first released its fence")
	default:
	}
	close(releaseFirst)
	<-secondEntered
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func (store *readinessStoreFake) ListObjects(
	_ context.Context,
	_ string,
	options minio.ListObjectsOptions,
) <-chan minio.ObjectInfo {
	store.listOptions = append(store.listOptions, options)
	result := make(chan minio.ObjectInfo, readinessMaximumVersions+1)
	if store.listCall < len(store.steps) {
		for _, object := range store.steps[store.listCall].objects {
			result <- object
		}
	}
	store.listCall++
	close(result)
	return result
}

func (store *readinessStoreFake) RemoveObject(
	_ context.Context,
	_ string,
	key string,
	options minio.RemoveObjectOptions,
) error {
	if store.removeError != nil {
		return store.removeError
	}
	store.removed = append(store.removed, minio.ObjectInfo{Key: key, VersionID: options.VersionID})
	return nil
}

func TestReadinessReconcilesDurableVersionsAndDeleteMarkers(t *testing.T) {
	t.Parallel()

	store := &readinessStoreFake{steps: []readinessStoreStep{
		{objects: []minio.ObjectInfo{
			{Key: readinessObjectKey, VersionID: "version-1"},
			{Key: readinessObjectKey, VersionID: "marker-2", IsDeleteMarker: true},
		}},
		{},
	}}
	if err := reconcileReadinessObjects(context.Background(), store, "bucket"); err != nil {
		t.Fatal(err)
	}
	if len(store.removed) != 2 || store.removed[0].VersionID != "version-1" ||
		store.removed[1].VersionID != "marker-2" || store.listCall != 2 {
		t.Fatalf("durable readiness cleanup is incomplete: %#v", store)
	}
	for _, options := range store.listOptions {
		if options.Prefix != readinessObjectPrefix || !options.WithVersions || !options.Recursive {
			t.Fatalf("readiness did not list the exact versioned prefix: %#v", options)
		}
	}
}

func TestReadinessCleanupIsBoundedAndFailClosed(t *testing.T) {
	t.Parallel()

	overflow := make([]minio.ObjectInfo, readinessMaximumVersions+1)
	for index := range overflow {
		overflow[index] = minio.ObjectInfo{Key: readinessObjectKey, VersionID: "version"}
	}
	store := &readinessStoreFake{steps: []readinessStoreStep{{objects: overflow}}}
	if err := reconcileReadinessObjects(context.Background(), store, "bucket"); !errors.Is(err, errReadinessCleanup) || len(store.removed) != readinessMaximumVersions {
		t.Fatalf("overflowing readiness cleanup did not make bounded recovery progress: %v, %d", err, len(store.removed))
	}

	store = &readinessStoreFake{steps: []readinessStoreStep{{objects: []minio.ObjectInfo{
		{Key: readinessObjectKey, VersionID: "version"},
	}}}, removeError: errors.New("delete denied")}
	if err := reconcileReadinessObjects(context.Background(), store, "bucket"); !errors.Is(err, errReadinessCleanup) {
		t.Fatalf("failed exact delete did not fail readiness closed: %v", err)
	}
}

func TestReadinessRecoversAmbiguousDeleteBeforeNextPut(t *testing.T) {
	t.Parallel()

	orphan := minio.ObjectInfo{Key: readinessObjectKey, VersionID: "ambiguous-version"}
	store := &readinessStoreFake{
		steps:       []readinessStoreStep{{objects: []minio.ObjectInfo{orphan}}, {objects: []minio.ObjectInfo{orphan}}, {}},
		removeError: errors.New("ambiguous delete"),
	}
	if err := reconcileReadinessObjects(context.Background(), store, "bucket"); !errors.Is(err, errReadinessCleanup) {
		t.Fatalf("ambiguous delete was not fail-closed: %v", err)
	}
	store.removeError = nil
	if err := reconcileReadinessObjects(context.Background(), store, "bucket"); err != nil {
		t.Fatalf("next fenced probe did not recover the durable orphan: %v", err)
	}
	if len(store.removed) != 1 || store.removed[0].VersionID != orphan.VersionID {
		t.Fatalf("recovery did not delete the exact orphan version: %#v", store.removed)
	}
}
