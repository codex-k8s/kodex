package credential

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/exactkubernetessecret"
	"github.com/google/uuid"
)

type fakeDirectBoundary struct {
	snapshot exactkubernetessecret.Snapshot
}

func (boundary *fakeDirectBoundary) Read(context.Context) (exactkubernetessecret.Snapshot, error) {
	return exactkubernetessecret.Snapshot{ResourceVersion: boundary.snapshot.ResourceVersion, Data: bytes.Clone(boundary.snapshot.Data)}, nil
}
func (boundary *fakeDirectBoundary) CompareAndSwap(_ context.Context, expected string, data []byte) (exactkubernetessecret.Snapshot, error) {
	if expected != boundary.snapshot.ResourceVersion {
		return exactkubernetessecret.Snapshot{}, errors.New("CAS mismatch")
	}
	version, _ := strconv.Atoi(expected)
	boundary.snapshot = exactkubernetessecret.Snapshot{ResourceVersion: strconv.Itoa(version + 1), Data: bytes.Clone(data)}
	return boundary.Read(context.Background())
}
func (*fakeDirectBoundary) Check(context.Context) error { return nil }
func (*fakeDirectBoundary) Close()                      {}

func TestDirectStoreRecoveryRestartAndRevoke(t *testing.T) {
	t.Parallel()
	raw, err := exactkubernetessecret.EncodeAggregate(exactkubernetessecret.NewAggregate(), maximumDirectCredentialRecords)
	if err != nil {
		t.Fatal(err)
	}
	boundary := &fakeDirectBoundary{snapshot: exactkubernetessecret.Snapshot{ResourceVersion: "1", Data: raw}}
	first := &DirectStore{boundary: boundary, resource: "interaction-gateway-bot-credentials", dataKey: "state.json"}
	bindingID, token := uuid.NewString(), "bot-token"
	materialized, err := first.MaterializeBotToken(context.Background(), bindingID, token)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &DirectStore{boundary: boundary, resource: first.resource, dataKey: first.dataKey}
	recovered, err := restarted.RecoverBotToken(context.Background(), bindingID)
	if err != nil || recovered != materialized {
		t.Fatalf("restart recovery=%+v err=%v, want %+v", recovered, err, materialized)
	}
	readback, err := restarted.ReadBotToken(context.Background(), bindingID, materialized.Version, materialized.ContentSHA256)
	if err != nil || readback != token {
		t.Fatalf("readback=%q err=%v", readback, err)
	}
	changed, err := restarted.RevokeBotToken(context.Background(), bindingID, materialized.Version)
	if err != nil || !changed {
		t.Fatalf("revoke changed=%v err=%v", changed, err)
	}
	if err := first.CheckBotTokenRevoked(context.Background(), bindingID, materialized.Version); err != nil {
		t.Fatal(err)
	}
	if changed, err = restarted.RevokeBotToken(context.Background(), bindingID, materialized.Version); err != nil || changed {
		t.Fatalf("idempotent revoke changed=%v err=%v", changed, err)
	}
	if _, err := restarted.RecoverBotToken(context.Background(), uuid.NewString()); err == nil {
		t.Fatal("unknown binding was recovered")
	}
}
