package stagingcrypto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

const (
	maximumKeyringBytes = 64 << 10
	maximumKeyringKeys  = 128
)

type keyringDocument struct {
	Version  int           `json:"version"`
	Revision int64         `json:"revision"`
	Current  string        `json:"current"`
	Keys     []keyDocument `json:"keys"`
}

type keyDocument struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Material   []byte `json:"material"`
}

type FileKeys struct {
	path  string
	guard secretdrafts.KeyGuard
}

func NewFileKeys(path string, guard secretdrafts.KeyGuard) (*FileKeys, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || guard == nil {
		return nil, ErrEncryptionUnavailable
	}
	return &FileKeys{path: path, guard: guard}, nil
}

func (source *FileKeys) Current(ctx context.Context) (value.DraftEncryptionKey, []byte, error) {
	document, manifest, err := source.load(ctx)
	defer clearKeyring(&document)
	if err != nil {
		return value.DraftEncryptionKey{}, nil, err
	}
	for _, key := range document.Keys {
		if key.ID == manifest.Current.ID {
			return manifest.Current, append([]byte(nil), key.Material...), nil
		}
	}
	return value.DraftEncryptionKey{}, nil, ErrEncryptionUnavailable
}

func (source *FileKeys) Resolve(ctx context.Context, wanted value.DraftEncryptionKey) ([]byte, error) {
	document, _, err := source.load(ctx)
	defer clearKeyring(&document)
	if err != nil {
		return nil, err
	}
	for _, key := range document.Keys {
		if key.ID == wanted.ID && key.Generation == wanted.Generation {
			return append([]byte(nil), key.Material...), nil
		}
	}
	return nil, ErrEncryptionUnavailable
}

func (source *FileKeys) ReserveEncryption(ctx context.Context, key value.DraftEncryptionKey) error {
	if err := source.guard.Reserve(ctx, key); err != nil {
		return ErrEncryptionUnavailable
	}
	return nil
}

func (source *FileKeys) Check(ctx context.Context) error {
	document, _, err := source.load(ctx)
	clearKeyring(&document)
	return err
}

func (source *FileKeys) load(ctx context.Context) (keyringDocument, value.DraftKeyManifest, error) {
	if err := ctx.Err(); err != nil {
		return keyringDocument{}, value.DraftKeyManifest{}, err
	}
	data, err := securefile.Read(source.path, maximumKeyringBytes)
	defer clear(data)
	if err != nil {
		return keyringDocument{}, value.DraftKeyManifest{}, ErrEncryptionUnavailable
	}
	document, manifest, err := decodeKeyring(data)
	if err != nil {
		return keyringDocument{}, value.DraftKeyManifest{}, err
	}
	if err := source.guard.Observe(ctx, manifest); err != nil {
		clearKeyring(&document)
		return keyringDocument{}, value.DraftKeyManifest{}, ErrEncryptionUnavailable
	}
	return document, manifest, nil
}

func decodeKeyring(data []byte) (keyringDocument, value.DraftKeyManifest, error) {
	var document keyringDocument
	invalid := func() (keyringDocument, value.DraftKeyManifest, error) {
		clearKeyring(&document)
		return keyringDocument{}, value.DraftKeyManifest{}, ErrEncryptionUnavailable
	}
	if len(data) == 0 || len(data) > maximumKeyringBytes || internalrpcauth.DecodeStrictJSON(data, &document) != nil ||
		document.Version != 1 || document.Revision < 1 || len(document.Keys) == 0 || len(document.Keys) > maximumKeyringKeys {
		return invalid()
	}
	manifest := value.DraftKeyManifest{Revision: document.Revision}
	seen := make(map[string]bool, len(document.Keys))
	generations := make(map[int64]bool, len(document.Keys))
	var maximumGeneration int64
	for _, key := range document.Keys {
		identity := value.DraftEncryptionKey{ID: key.ID, Generation: key.Generation}
		digest := sha256.Sum256(key.Material)
		if !validKeyIdentity(identity) || len(key.Material) != 32 || hex.EncodeToString(digest[:]) != key.ID ||
			seen[key.ID] || generations[key.Generation] || key.Generation > document.Revision {
			return invalid()
		}
		seen[key.ID], generations[key.Generation] = true, true
		manifest.Keys = append(manifest.Keys, identity)
		if key.Generation > maximumGeneration {
			maximumGeneration = key.Generation
		}
		if key.ID == document.Current {
			manifest.Current = identity
		}
	}
	if manifest.Current.ID == "" || manifest.Current.Generation != maximumGeneration {
		return invalid()
	}
	sort.Slice(manifest.Keys, func(i, j int) bool { return manifest.Keys[i].Generation < manifest.Keys[j].Generation })
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return invalid()
	}
	digest := sha256.Sum256(canonical)
	manifest.Digest = hex.EncodeToString(digest[:])
	return document, manifest, nil
}

func clearKeyring(document *keyringDocument) {
	for index := range document.Keys {
		clear(document.Keys[index].Material)
	}
}
