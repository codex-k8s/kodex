package credential

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/exactkubernetessecret"
	domaincredential "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/credential"
	"github.com/google/uuid"
)

const maximumDirectCredentialRecords = 1024

type directBoundary interface {
	Read(context.Context) (exactkubernetessecret.Snapshot, error)
	CompareAndSwap(context.Context, string, []byte) (exactkubernetessecret.Snapshot, error)
	Check(context.Context) error
	Close()
}

// DirectStore хранит bot credentials в одном exact aggregate Secret. bindingID
// выводится domain service из server-owned operation/provider state.
type DirectStore struct {
	boundary directBoundary
	resource string
	dataKey  string
}

func NewDirect(resourceName, dataKey string, timeout time.Duration) (*DirectStore, error) {
	if resourceName != "interaction-gateway-bot-credentials" || dataKey != "state.json" {
		return nil, errors.New("direct bot credential registry is invalid")
	}
	client, err := exactkubernetessecret.New(exactkubernetessecret.Config{
		ResourceName: resourceName, DataKey: dataKey, Timeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return &DirectStore{boundary: client, resource: resourceName, dataKey: dataKey}, nil
}

func (store *DirectStore) MaterializeBotToken(ctx context.Context, bindingID, token string) (domaincredential.Materialized, error) {
	if uuid.Validate(bindingID) != nil || token == "" || len(token) > 4096 {
		return domaincredential.Materialized{}, errors.New("direct bot credential materialization input is invalid")
	}
	snapshot, document, err := store.read(ctx)
	if err != nil {
		return domaincredential.Materialized{}, err
	}
	digest := exactkubernetessecret.ValueSHA256([]byte(token))
	if record, ok := document.Records[bindingID]; ok {
		if record.Status != exactkubernetessecret.RecordActive || record.ContentSHA256 != digest || !bytes.Equal(record.Value, []byte(token)) {
			return domaincredential.Materialized{}, errors.New("direct bot immutable credential conflict")
		}
		return store.materialized(bindingID, record), nil
	}
	next := exactkubernetessecret.CloneAggregate(document)
	next.Generation++
	next.Records[bindingID] = exactkubernetessecret.Record{
		Version: 1, Status: exactkubernetessecret.RecordActive,
		ContentSHA256: digest, Value: []byte(token),
	}
	served, err := store.write(ctx, snapshot, document, next)
	if err != nil {
		return domaincredential.Materialized{}, err
	}
	record := served.Records[bindingID]
	if record.Status != exactkubernetessecret.RecordActive || record.Version != 1 ||
		record.ContentSHA256 != digest || !bytes.Equal(record.Value, []byte(token)) {
		return domaincredential.Materialized{}, errors.New("direct bot credential readback mismatch")
	}
	return store.materialized(bindingID, record), nil
}

func (store *DirectStore) RecoverBotToken(ctx context.Context, bindingID string) (domaincredential.Materialized, error) {
	if uuid.Validate(bindingID) != nil {
		return domaincredential.Materialized{}, errors.New("direct bot credential recovery input is invalid")
	}
	_, document, err := store.read(ctx)
	if err != nil {
		return domaincredential.Materialized{}, err
	}
	record, ok := document.Records[bindingID]
	if !ok || record.Status != exactkubernetessecret.RecordActive {
		return domaincredential.Materialized{}, errors.New("recover direct bot credential")
	}
	return store.materialized(bindingID, record), nil
}

func (store *DirectStore) ReadBotToken(ctx context.Context, bindingID string, expectedVersion uint64, expectedSHA string) (string, error) {
	if uuid.Validate(bindingID) != nil || expectedVersion == 0 || len(expectedSHA) != 64 {
		return "", errors.New("direct bot credential read input is invalid")
	}
	_, document, err := store.read(ctx)
	if err != nil {
		return "", err
	}
	record, ok := document.Records[bindingID]
	if !ok || record.Status != exactkubernetessecret.RecordActive || record.Version != expectedVersion ||
		record.ContentSHA256 != expectedSHA || exactkubernetessecret.ValueSHA256(record.Value) != expectedSHA {
		return "", errors.New("direct bot credential readback mismatch")
	}
	return string(record.Value), nil
}

func (store *DirectStore) RevokeBotToken(ctx context.Context, bindingID string, activeVersion uint64) (bool, error) {
	if uuid.Validate(bindingID) != nil || activeVersion == 0 {
		return false, errors.New("direct bot credential revoke input is invalid")
	}
	snapshot, document, err := store.read(ctx)
	if err != nil {
		return false, err
	}
	record, ok := document.Records[bindingID]
	if !ok {
		return false, errors.New("direct bot credential is unavailable")
	}
	if record.Status == exactkubernetessecret.RecordRevoked && record.Version > activeVersion {
		return false, nil
	}
	if record.Status != exactkubernetessecret.RecordActive || record.Version != activeVersion || record.Version == ^uint64(0) {
		return false, errors.New("direct bot credential revoke CAS rejected")
	}
	next := exactkubernetessecret.CloneAggregate(document)
	next.Generation++
	record.Version++
	record.Status = exactkubernetessecret.RecordRevoked
	record.ContentSHA256 = strings.Repeat("0", 64)
	clear(record.Value)
	record.Value = nil
	next.Records[bindingID] = record
	served, err := store.write(ctx, snapshot, document, next)
	if err != nil {
		return false, err
	}
	readback := served.Records[bindingID]
	if readback.Status != exactkubernetessecret.RecordRevoked || readback.Version <= activeVersion || len(readback.Value) != 0 {
		return false, errors.New("direct bot credential revoke readback mismatch")
	}
	return true, nil
}

func (store *DirectStore) CheckBotTokenRevoked(ctx context.Context, bindingID string, activeVersion uint64) error {
	if uuid.Validate(bindingID) != nil || activeVersion == 0 {
		return errors.New("direct bot credential revoke readback input is invalid")
	}
	_, document, err := store.read(ctx)
	if err != nil {
		return err
	}
	record, ok := document.Records[bindingID]
	if !ok || record.Status != exactkubernetessecret.RecordRevoked || record.Version <= activeVersion ||
		len(record.Value) != 0 || record.ContentSHA256 != strings.Repeat("0", 64) {
		return errors.New("direct bot credential revoke readback mismatch")
	}
	return nil
}

func (store *DirectStore) Check(ctx context.Context) error {
	_, _, err := store.read(ctx)
	return err
}

func (store *DirectStore) Close() { store.boundary.Close() }

func (store *DirectStore) read(ctx context.Context) (exactkubernetessecret.Snapshot, exactkubernetessecret.Aggregate, error) {
	snapshot, err := store.boundary.Read(ctx)
	if err != nil {
		return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, err
	}
	document, err := exactkubernetessecret.DecodeAggregate(snapshot.Data, maximumDirectCredentialRecords)
	if err != nil {
		return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, err
	}
	for bindingID := range document.Records {
		if uuid.Validate(bindingID) != nil {
			return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, errors.New("direct bot credential aggregate contains an unknown binding")
		}
	}
	return snapshot, document, nil
}

func (store *DirectStore) write(ctx context.Context, snapshot exactkubernetessecret.Snapshot, previous, next exactkubernetessecret.Aggregate) (exactkubernetessecret.Aggregate, error) {
	if err := exactkubernetessecret.ValidateForwardTransition(previous, next); err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	raw, err := exactkubernetessecret.EncodeAggregate(next, maximumDirectCredentialRecords)
	if err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	served, err := store.boundary.CompareAndSwap(ctx, snapshot.ResourceVersion, raw)
	if err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	readback, err := exactkubernetessecret.DecodeAggregate(served.Data, maximumDirectCredentialRecords)
	if err != nil || readback.Generation != next.Generation || readback.DigestSHA256 == previous.DigestSHA256 {
		return exactkubernetessecret.Aggregate{}, errors.New("direct bot credential aggregate readback mismatch")
	}
	return readback, nil
}

func (store *DirectStore) materialized(bindingID string, record exactkubernetessecret.Record) domaincredential.Materialized {
	return domaincredential.Materialized{
		BindingID: bindingID,
		SecretRef: "kubernetes-secret://mattercodex-system/" + store.resource + "/" + store.dataKey + "#" + bindingID,
		Version:   record.Version, ContentSHA256: record.ContentSHA256,
	}
}
