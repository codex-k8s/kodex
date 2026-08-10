package secret

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/exactkubernetessecret"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	"github.com/google/uuid"
)

type fakeAggregateBoundary struct {
	snapshot exactkubernetessecret.Snapshot
	casErr   error
}

func (boundary *fakeAggregateBoundary) Read(context.Context) (exactkubernetessecret.Snapshot, error) {
	return exactkubernetessecret.Snapshot{ResourceVersion: boundary.snapshot.ResourceVersion, Data: bytes.Clone(boundary.snapshot.Data)}, nil
}

func (boundary *fakeAggregateBoundary) CompareAndSwap(_ context.Context, expected string, data []byte) (exactkubernetessecret.Snapshot, error) {
	if boundary.casErr != nil {
		return exactkubernetessecret.Snapshot{}, boundary.casErr
	}
	if expected != boundary.snapshot.ResourceVersion {
		return exactkubernetessecret.Snapshot{}, errors.New("CAS mismatch")
	}
	version, _ := strconv.Atoi(expected)
	boundary.snapshot = exactkubernetessecret.Snapshot{ResourceVersion: strconv.Itoa(version + 1), Data: bytes.Clone(data)}
	return boundary.Read(context.Background())
}

func (*fakeAggregateBoundary) Check(context.Context) error { return nil }
func (*fakeAggregateBoundary) Close()                      {}

func TestDirectProviderStoreRegistryCASAndRevoke(t *testing.T) {
	t.Parallel()
	raw, err := exactkubernetessecret.EncodeAggregate(exactkubernetessecret.NewAggregate(), maximumAggregateRecords)
	if err != nil {
		t.Fatal(err)
	}
	boundary := &fakeAggregateBoundary{snapshot: exactkubernetessecret.Snapshot{ResourceVersion: "1", Data: raw}}
	store := &KubernetesStore{boundary: boundary, prefix: "mattercodex/integration-gateway/provider-credentials"}
	ref := store.prefix + "/" + uuid.NewString() + "/" + uuid.NewString() + "/3"
	material := []byte("provider-credential")
	version, err := store.Put(context.Background(), ref, material)
	if err != nil || version.Version != 1 || version.ContentDigest != exactkubernetessecret.ValueSHA256(material) {
		t.Fatalf("materialize exact credential: version=%+v err=%v", version, err)
	}
	if _, _, err := store.Get(context.Background(), "other/"+ref); err == nil {
		t.Fatal("unknown provider path was accepted")
	}
	boundary.casErr = errors.New("synthetic CAS conflict")
	other := store.prefix + "/" + uuid.NewString() + "/" + uuid.NewString() + "/1"
	if _, err := store.Put(context.Background(), other, material); err == nil {
		t.Fatal("Kubernetes resourceVersion conflict was accepted")
	}
	boundary.casErr = nil
	if err := store.Revoke(context.Background(), ref, version.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(context.Background(), ref); !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("revoked credential read error=%v", err)
	}
}

func TestDirectProviderConstructorRejectsAlternateResource(t *testing.T) {
	t.Parallel()
	if _, err := NewKubernetesStore("other-secret", "state.json", "mattercodex/integration-gateway/provider-credentials", 5*time.Second); err == nil {
		t.Fatal("alternate Kubernetes Secret resource was accepted")
	}
}
