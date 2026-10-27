package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// workspaceObjectFixture injects lost acknowledgements and readback faults into the real object-storage port.
// Counters distinguish a read-only recovery from another PUT or an unsafe speculative DELETE.
// workspaceObjectFixture вводит lost ACK/readback faults через настоящий object-storage port.
// Counters отличают read-only recovery от повторного PUT или speculative DELETE.
type workspaceObjectFixture struct {
	objectstorage.Store
	puts, deletes                        int
	losePutACK, hideAfterPut, skipCommit bool
	corruptReadback, driftVersion        bool
}

// Put persists the requested bytes once before optionally losing the response, as a real unknown write can do.
// Put сохраняет requested bytes один раз до имитации потерянного ответа, как при настоящем unknown write.
func (store *workspaceObjectFixture) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.puts++
	if store.skipCommit {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	receipt, err := store.Store.Put(ctx, input)
	if err == nil && store.losePutACK {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	return receipt, err
}

// Head can hide a committed write without reporting it absent or allowing an automatic second PUT.
// Head может скрыть committed write, не выдавая его за absent и не разрешая второй PUT.
func (store *workspaceObjectFixture) Head(ctx context.Context, key, version string) (objectstorage.Receipt, error) {
	if store.hideAfterPut && store.puts > 0 {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	return store.Store.Head(ctx, key, version)
}

// Get returns a controlled corrupt body or version while preserving the original stored object.
// Get возвращает controlled corrupt body/version, сохраняя исходный stored object.
func (store *workspaceObjectFixture) Get(ctx context.Context, key, version string) (objectstorage.Object, error) {
	object, err := store.Store.Get(ctx, key, version)
	if err == nil && store.corruptReadback {
		_ = object.Body.Close()
		object.Body = io.NopCloser(strings.NewReader("changed"))
	}
	if err == nil && store.driftVersion {
		object.VersionID = "different-version"
	}
	return object, err
}

// Delete records any forbidden cleanup call instead of concealing it behind an in-memory adapter.
// Delete учитывает каждый forbidden cleanup call, не скрывая его за in-memory adapter.
func (store *workspaceObjectFixture) Delete(ctx context.Context, key, version string) error {
	store.deletes++
	return store.Store.Delete(ctx, key, version)
}

// TestWorkspaceObjectLostACKReconcilesExactBytesWithoutNewWrite exercises the real writer and exact GET path.
// Replaying the checkpoint key is read-only; conflicting bytes preserve the first object and fail closed.
// TestWorkspaceObjectLostACKReconcilesExactBytesWithoutNewWrite проверяет настоящий writer и exact GET.
// Повтор checkpoint key read-only; conflicting bytes сохраняют первый object и дают closed failure.
func TestWorkspaceObjectLostACKReconcilesExactBytesWithoutNewWrite(t *testing.T) {
	store := &workspaceObjectFixture{Store: objectstoragetest.New(), losePutACK: true}
	key := "session-archive/v2/synthetic/checkpoint/part-1"
	raw := []byte("exact unpublished source")
	first, err := writeWorkspaceObject(t.Context(), store, key, "application/x-tar", raw, runtimecontract.MaximumWorkspaceArchivePartBytes, true)
	if err != nil || first.Version == "" || store.puts != 1 || store.deletes != 0 {
		t.Fatal("lost acknowledgement did not reconcile the committed exact object")
	}
	again, err := writeWorkspaceObject(t.Context(), store, key, "application/x-tar", raw, runtimecontract.MaximumWorkspaceArchivePartBytes, false)
	if err != nil || first != again || store.puts != 1 || store.deletes != 0 {
		t.Fatal("replay performed a write or changed the immutable object receipt")
	}
	if _, err := writeWorkspaceObject(t.Context(), store, key, "application/x-tar", []byte("different source"), runtimecontract.MaximumWorkspaceArchivePartBytes, false); err == nil || store.puts != 1 || store.deletes != 0 {
		t.Fatal("a conflicting checkpoint replaced or deleted the existing source")
	}
	object, err := store.Store.Get(t.Context(), first.Key, first.Version)
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(object.Body)
	closeErr := object.Body.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(actual, raw) {
		t.Fatal("failed reconciliation changed source custody")
	}
}

// TestWorkspaceObjectUnknownOrCorruptReadbackNeverCleansUp checks fixed errors, bounds and retained custody.
// Recovery after an unresolved write cannot infer absence or create another version or prefix.
// TestWorkspaceObjectUnknownOrCorruptReadbackNeverCleansUp проверяет closed errors, bounds и retained custody.
// Recovery unknown write не выводит absence и не создаёт ещё одну version/prefix.
func TestWorkspaceObjectUnknownOrCorruptReadbackNeverCleansUp(t *testing.T) {
	for _, name := range []string{"unknown", "corrupt", "version", "absent_after_unknown"} {
		t.Run(name, func(t *testing.T) {
			store := &workspaceObjectFixture{Store: objectstoragetest.New(), losePutACK: true,
				hideAfterPut: name == "unknown", corruptReadback: name == "corrupt", driftVersion: name == "version", skipCommit: name == "absent_after_unknown"}
			key := "session-archive/v2/synthetic/checkpoint/rootmanifest.json"
			for attempt := 0; attempt < 2; attempt++ {
				_, err := writeWorkspaceObject(t.Context(), store, key, "application/json", []byte("source"), runtimecontract.MaximumWorkspaceManifestBytes, attempt == 0)
				if !errors.Is(err, errWorkspaceObjectUnconfirmed) || strings.Contains(err.Error(), key) || store.puts != 1 || store.deletes != 0 {
					t.Fatal("uncertain custody caused a blind write, cleanup or private error disclosure")
				}
			}
		})
	}
	store := &workspaceObjectFixture{Store: objectstoragetest.New()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := writeWorkspaceObject(ctx, store, "session-archive/v2/synthetic/key", "application/json", []byte("x"), 1, true); err == nil || store.puts != 0 {
		t.Fatal("cancelled checkpoint performed a write")
	}
	if _, err := writeWorkspaceObject(t.Context(), store, "session-archive/v2/synthetic/key", "application/json", []byte("xx"), 1, true); err == nil || store.puts != 0 {
		t.Fatal("oversized checkpoint performed a write")
	}
}
