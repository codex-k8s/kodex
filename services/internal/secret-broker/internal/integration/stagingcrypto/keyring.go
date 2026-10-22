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
	// maximumKeyringBytes caps encoded runtime and administrative keyring input at 64 KiB before decoding.
	// maximumKeyringBytes ограничивает encoded runtime и administrative keyring input до 64 КиБ перед
	// декодированием.
	maximumKeyringBytes = 64 << 10
	// maximumKeyringKeys caps retained key entries at 128; rotation refuses capacity exhaustion rather than
	// silently retiring read keys.
	// maximumKeyringKeys ограничивает retained key entries до 128; rotation отказывает при исчерпании ёмкости
	// вместо скрытого удаления read keys.
	maximumKeyringKeys = 128
)

// keyringDocument is the strict versioned JSON envelope containing current/retained key material; it must
// never be logged or returned as an ordinary API DTO.
// keyringDocument — строгий versioned JSON envelope с current/retained key material; его нельзя логировать
// и возвращать как обычный API DTO.
type keyringDocument struct {
	Version  int           `json:"version"`
	Revision int64         `json:"revision"`
	Current  string        `json:"current"`
	Keys     []keyDocument `json:"keys"`
}

// keyDocument pairs sensitive 32-byte material with its SHA-256 ID and positive generation; decoded
// material slices require explicit clearing.
// keyDocument связывает чувствительный 32-байтовый material с SHA-256 ID и положительным generation;
// decoded material slices требуют явной очистки.
type keyDocument struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
	Material   []byte `json:"material"`
}

// FileKeys rereads a configured keyring and delegates durable rollback/use checks to KeyGuard; it caches
// neither material nor a local authority counter.
// FileKeys повторно читает настроенный keyring и делегирует устойчивые rollback/use checks KeyGuard; ни
// материал, ни локальный authority counter не кэширует.
type FileKeys struct {
	path  string
	guard secretdrafts.KeyGuard
}

// NewFileKeys accepts a clean absolute path and nonnil guard without opening the file or validating guard
// readiness; typed nils remain the caller's responsibility.
// NewFileKeys принимает clean absolute path и nonnil guard без открытия файла и проверки guard readiness;
// typed nil остаётся ответственностью caller.
func NewFileKeys(path string, guard secretdrafts.KeyGuard) (*FileKeys, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || guard == nil {
		return nil, ErrEncryptionUnavailable
	}
	return &FileKeys{path: path, guard: guard}, nil
}

// Current loads and observes the manifest, copies the selected current material and clears the decoded
// keyring before returning; reservation is a separate step.
// Current загружает и учитывает manifest, копирует выбранный current material и очищает decoded keyring до
// return; reservation выполняется отдельно.
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

// Resolve loads/observes the latest manifest and copies only the matching retained ID/generation, clearing
// the decoded keyring without consuming write budget.
// Resolve загружает/учитывает последний manifest и копирует лишь совпавший retained ID/generation, очищая
// decoded keyring без расхода write budget.
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

// ReserveEncryption delegates the exact identity to the durable guard and maps every failure to the fixed
// unavailable error.
// ReserveEncryption передаёт точную identity устойчивому guard и отображает любой failure в фиксированную
// unavailable error.
func (source *FileKeys) ReserveEncryption(ctx context.Context, key value.DraftEncryptionKey) error {
	if err := source.guard.Reserve(ctx, key); err != nil {
		return ErrEncryptionUnavailable
	}
	return nil
}

// Check loads/observes and clears the keyring, then verifies current-key readiness without reserving
// encryption use; it can update manifest guard state.
// Check загружает/учитывает и очищает keyring, затем проверяет current-key readiness без резервирования
// использования; может обновлять manifest guard state.
func (source *FileKeys) Check(ctx context.Context) error {
	document, manifest, err := source.load(ctx)
	clearKeyring(&document)
	if err != nil {
		return err
	}
	if source.guard.CheckCurrent(ctx, manifest.Current) != nil {
		return ErrEncryptionUnavailable
	}
	return nil
}

// load checks initial cancellation, reads a bounded securefile, strictly decodes it and observes the
// durable manifest before returning material; failures clear raw/decoded copies and never fall back to an
// older keyring.
// load проверяет исходную cancellation, читает bounded securefile, строго декодирует и учитывает durable
// manifest до возврата материала; при отказе очищает raw/decoded copies и не откатывается к старому
// keyring.
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

// decodeKeyring strictly validates version/revision/count, unique material-bound IDs/generations and the
// highest-generation current key. It sorts metadata to hash a key-free manifest, permits revision gaps and
// clears decoded material on invalid input.
// decodeKeyring строго проверяет version/revision/count, уникальные связанные с материалом IDs/generations
// и current key с максимальным generation. Сортирует metadata для хеша manifest без материала, допускает
// разрывы revision и очищает decoded material при ошибке.
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

// clearKeyring overwrites each decoded material slice without removing metadata or claiming to erase other
// copies or expanded AES state.
// clearKeyring обнуляет каждый decoded material slice без удаления metadata и без гарантии стирания иных
// копий и expanded AES state.
func clearKeyring(document *keyringDocument) {
	for index := range document.Keys {
		clear(document.Keys[index].Material)
	}
}
