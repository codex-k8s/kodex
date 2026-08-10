package secret

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/exactkubernetessecret"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	"github.com/google/uuid"
)

const (
	maximumAggregateRecords = 1024
	gitCredentialMountRoot  = "/var/run/secrets/mattercodex/integration-gateway/git-credentials"
)

type aggregateBoundary interface {
	Read(context.Context) (exactkubernetessecret.Snapshot, error)
	CompareAndSwap(context.Context, string, []byte) (exactkubernetessecret.Snapshot, error)
	Check(context.Context) error
	Close()
}

type KubernetesStore struct {
	boundary aggregateBoundary
	prefix   string
}

func NewKubernetesStore(resourceName, dataKey, pathPrefix string, timeout time.Duration) (*KubernetesStore, error) {
	if resourceName != "integration-gateway-provider-credentials" || dataKey != "state.json" || !validProviderPrefix(pathPrefix) {
		return nil, errors.New("direct provider credential registry is invalid")
	}
	client, err := exactkubernetessecret.New(exactkubernetessecret.Config{
		ResourceName: resourceName, DataKey: dataKey, Timeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	return &KubernetesStore{boundary: client, prefix: pathPrefix}, nil
}

func (store *KubernetesStore) Put(ctx context.Context, ref string, value []byte) (secretstore.Version, error) {
	if !store.validRef(ref) || len(value) == 0 || len(value) > 64<<10 {
		return secretstore.Version{}, errors.New("direct provider credential input is invalid")
	}
	snapshot, document, err := store.read(ctx)
	if err != nil {
		return secretstore.Version{}, err
	}
	digest := exactkubernetessecret.ValueSHA256(value)
	if record, ok := document.Records[ref]; ok {
		if record.Status != exactkubernetessecret.RecordActive || record.ContentSHA256 != digest || !bytes.Equal(record.Value, value) {
			return secretstore.Version{}, errors.New("direct provider immutable credential conflict")
		}
		return version(ref, record), nil
	}
	next := exactkubernetessecret.CloneAggregate(document)
	next.Generation++
	next.Records[ref] = exactkubernetessecret.Record{
		Version: 1, Status: exactkubernetessecret.RecordActive, ContentSHA256: digest, Value: bytes.Clone(value),
	}
	served, err := store.write(ctx, snapshot, document, next)
	if err != nil {
		return secretstore.Version{}, err
	}
	record := served.Records[ref]
	if record.Status != exactkubernetessecret.RecordActive || record.Version != 1 || record.ContentSHA256 != digest || !bytes.Equal(record.Value, value) {
		return secretstore.Version{}, errors.New("direct provider credential readback mismatch")
	}
	return version(ref, record), nil
}

func (store *KubernetesStore) Get(ctx context.Context, ref string) ([]byte, secretstore.Version, error) {
	if !store.validRef(ref) {
		return nil, secretstore.Version{}, errors.New("direct provider credential reference is invalid")
	}
	_, document, err := store.read(ctx)
	if err != nil {
		return nil, secretstore.Version{}, err
	}
	record, ok := document.Records[ref]
	if !ok || record.Status == exactkubernetessecret.RecordRevoked {
		return nil, secretstore.Version{}, secretstore.ErrNotFound
	}
	return bytes.Clone(record.Value), version(ref, record), nil
}

func (store *KubernetesStore) Revoke(ctx context.Context, ref string, activeVersion uint64) error {
	if !store.validRef(ref) || activeVersion == 0 {
		return errors.New("direct provider credential revoke input is invalid")
	}
	snapshot, document, err := store.read(ctx)
	if err != nil {
		return err
	}
	record, ok := document.Records[ref]
	if !ok {
		return secretstore.ErrNotFound
	}
	if record.Status == exactkubernetessecret.RecordRevoked && record.Version > activeVersion {
		return nil
	}
	if record.Status != exactkubernetessecret.RecordActive || record.Version != activeVersion || record.Version == ^uint64(0) {
		return errors.New("direct provider credential revoke CAS rejected")
	}
	next := exactkubernetessecret.CloneAggregate(document)
	next.Generation++
	record.Version++
	record.Status = exactkubernetessecret.RecordRevoked
	record.ContentSHA256 = strings.Repeat("0", 64)
	clear(record.Value)
	record.Value = nil
	next.Records[ref] = record
	served, err := store.write(ctx, snapshot, document, next)
	if err != nil {
		return err
	}
	readback := served.Records[ref]
	if readback.Status != exactkubernetessecret.RecordRevoked || readback.Version <= activeVersion || len(readback.Value) != 0 {
		return errors.New("direct provider credential revoke readback mismatch")
	}
	return nil
}

func (store *KubernetesStore) Check(ctx context.Context) error {
	_, _, err := store.read(ctx)
	return err
}

func (store *KubernetesStore) Close() { store.boundary.Close() }

func (store *KubernetesStore) read(ctx context.Context) (exactkubernetessecret.Snapshot, exactkubernetessecret.Aggregate, error) {
	snapshot, err := store.boundary.Read(ctx)
	if err != nil {
		return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, err
	}
	document, err := exactkubernetessecret.DecodeAggregate(snapshot.Data, maximumAggregateRecords)
	if err != nil {
		return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, err
	}
	for ref := range document.Records {
		if !store.validRef(ref) {
			return exactkubernetessecret.Snapshot{}, exactkubernetessecret.Aggregate{}, errors.New("direct provider credential registry contains an unknown ref")
		}
	}
	return snapshot, document, nil
}

func (store *KubernetesStore) write(ctx context.Context, snapshot exactkubernetessecret.Snapshot, previous, next exactkubernetessecret.Aggregate) (exactkubernetessecret.Aggregate, error) {
	if err := exactkubernetessecret.ValidateForwardTransition(previous, next); err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	raw, err := exactkubernetessecret.EncodeAggregate(next, maximumAggregateRecords)
	if err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	served, err := store.boundary.CompareAndSwap(ctx, snapshot.ResourceVersion, raw)
	if err != nil {
		return exactkubernetessecret.Aggregate{}, err
	}
	readback, err := exactkubernetessecret.DecodeAggregate(served.Data, maximumAggregateRecords)
	if err != nil || readback.Generation != next.Generation || readback.DigestSHA256 == previous.DigestSHA256 {
		return exactkubernetessecret.Aggregate{}, errors.New("direct provider aggregate readback mismatch")
	}
	return readback, nil
}

func (store *KubernetesStore) validRef(ref string) bool {
	parts := strings.Split(ref, "/")
	prefix := strings.Split(store.prefix, "/")
	if len(parts) != len(prefix)+3 || strings.Join(parts[:len(prefix)], "/") != store.prefix ||
		uuid.Validate(parts[len(prefix)]) != nil || uuid.Validate(parts[len(prefix)+1]) != nil {
		return false
	}
	generationRaw := parts[len(prefix)+2]
	generation, err := strconv.ParseUint(generationRaw, 10, 64)
	return err == nil && generation > 0 && strconv.FormatUint(generation, 10) == generationRaw && strings.Join(parts, "/") == ref
}

func validProviderPrefix(prefix string) bool {
	return prefix == "mattercodex/integration-gateway/provider-credentials"
}

type FileStore struct {
	path     string
	registry map[string]uint64
}

func NewGitFileStore(path string, registry map[string]uint64) (*FileStore, error) {
	if path != gitCredentialMountRoot+string(os.PathSeparator)+"state.json" || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		len(registry) == 0 || len(registry) > 32 {
		return nil, errors.New("direct Git credential file registry is invalid")
	}
	clone := make(map[string]uint64, len(registry))
	for ref, expectedVersion := range registry {
		if ref == "" || expectedVersion == 0 || strings.ContainsAny(ref, "\x00\r\n") {
			return nil, errors.New("direct Git credential file registry is invalid")
		}
		clone[ref] = expectedVersion
	}
	return &FileStore{path: path, registry: clone}, nil
}

func (*FileStore) Put(context.Context, string, []byte) (secretstore.Version, error) {
	return secretstore.Version{}, errors.New("direct Git credential store is read-only")
}

func (store *FileStore) Get(_ context.Context, ref string) ([]byte, secretstore.Version, error) {
	expectedVersion, ok := store.registry[ref]
	if !ok {
		return nil, secretstore.Version{}, errors.New("direct Git credential ref is not registered")
	}
	document, err := store.read()
	if err != nil {
		return nil, secretstore.Version{}, err
	}
	record, ok := document.Records[ref]
	if !ok || record.Status != exactkubernetessecret.RecordActive || record.Version != expectedVersion {
		return nil, secretstore.Version{}, errors.New("direct Git credential version-pinned readback mismatch")
	}
	return bytes.Clone(record.Value), version(ref, record), nil
}

func (*FileStore) Revoke(context.Context, string, uint64) error {
	return errors.New("direct Git credential store is read-only")
}

func (store *FileStore) Check(context.Context) error {
	_, err := store.read()
	return err
}

func (*FileStore) Close() {}

func (store *FileStore) read() (exactkubernetessecret.Aggregate, error) {
	info, err := os.Stat(store.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return exactkubernetessecret.Aggregate{}, errors.New("direct Git credential file is unsafe")
	}
	file, err := os.Open(store.path)
	if err != nil {
		return exactkubernetessecret.Aggregate{}, errors.New("read direct Git credential file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return exactkubernetessecret.Aggregate{}, errors.New("read direct Git credential file")
	}
	document, err := exactkubernetessecret.DecodeAggregate(raw, 32)
	if err != nil || len(document.Records) != len(store.registry) {
		return exactkubernetessecret.Aggregate{}, errors.New("direct Git credential aggregate is invalid")
	}
	for ref, record := range document.Records {
		expectedVersion, ok := store.registry[ref]
		if !ok || record.Status != exactkubernetessecret.RecordActive || record.Version != expectedVersion {
			return exactkubernetessecret.Aggregate{}, errors.New("direct Git credential aggregate contains an unknown record")
		}
	}
	return document, nil
}

func version(ref string, record exactkubernetessecret.Record) secretstore.Version {
	return secretstore.Version{Ref: ref, Version: record.Version, ContentDigest: record.ContentSHA256}
}
