// Package stagingguard persists key-generation metadata and encryption-use budgets in a dedicated
// ConfigMap without transporting key material.
// Package stagingguard хранит metadata поколений ключей и бюджет шифрования в отдельном ConfigMap
// без передачи материала ключей.
package stagingguard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	// MaximumEncryptions caps durable reservations per key at 2^24, stricter than the random-nonce GCM upper
	// bound; failed/uncertain reservations are not refunded.
	// MaximumEncryptions ограничивает устойчивые reservations одного ключа до 2^24, строже предела
	// random-nonce GCM; failed/uncertain reservations не возвращаются.
	MaximumEncryptions uint64 = 1 << 24
	// StateKey is the sole ConfigMap data entry holding versioned manifest/counter metadata, never key
	// material.
	// StateKey — единственная ConfigMap data entry с versioned manifest/counter metadata, без материала
	// ключей.
	StateKey = "state.json"
	// OwnerLabel selects the metadata key used to verify bootstrap management of the guard object.
	// OwnerLabel задаёт metadata key для проверки управления guard object со стороны bootstrap.
	OwnerLabel = "app.kubernetes.io/managed-by"
	// OwnerValue is the exact expected bootstrap-manager label value, not an identity supplied by the command
	// caller.
	// OwnerValue — точное ожидаемое значение bootstrap-manager label, не identity, назначаемая command caller.
	OwnerValue = "kodex-secret-broker-bootstrap"
	// PurposeLabel selects the label separating durable key-guard state from unrelated managed ConfigMaps.
	// PurposeLabel задаёт label, отделяющий устойчивое key-guard state от иных managed ConfigMaps.
	PurposeLabel = "kodex.dev/purpose"
	// PurposeValue is the exact required guard-purpose value; a common owner label alone is insufficient.
	// PurposeValue — точное обязательное значение guard purpose; одной общей owner label недостаточно.
	PurposeValue = "secret-draft-key-guard"
	// maximumManifestKeys bounds retained key metadata to 128 entries before accepting a manifest.
	// maximumManifestKeys ограничивает retained key metadata до 128 entries перед принятием manifest.
	maximumManifestKeys = 128
	// maximumStateBytes caps both decoded and newly serialized guard-state JSON at 64 KiB.
	// maximumStateBytes ограничивает decoded и вновь serialized guard-state JSON до 64 КиБ.
	maximumStateBytes = 64 << 10
	// maximumAttempts bounds fresh Get/Update CAS attempts to eight; only explicit Conflict triggers another
	// attempt.
	// maximumAttempts ограничивает fresh Get/Update CAS до восьми попыток; новую попытку вызывает лишь явный
	// Conflict.
	maximumAttempts = 8
	// operationTimeout bounds one guard mutation/readiness operation to five seconds, shortened by the
	// caller's cancellation or earlier deadline.
	// operationTimeout ограничивает одну guard mutation/readiness operation пятью секундами, с учётом отмены и
	// более раннего caller deadline.
	operationTimeout = 5 * time.Second
)

// ErrUnavailable hides guard lookup/schema/manifest/CAS failures without exposing state metadata or
// Kubernetes diagnostics.
// ErrUnavailable скрывает guard lookup/schema/manifest/CAS failures без раскрытия state metadata и
// Kubernetes diagnostics.
var ErrUnavailable = errors.New("secret draft key guard is unavailable")

// ConfigMaps permits only Get/Update of an existing object; namespace binding belongs to trusted client
// construction, not a runtime payload.
// ConfigMaps разрешает лишь Get/Update существующего объекта; namespace binding задаётся доверенным
// созданием клиента, не runtime payload.
type ConfigMaps interface {
	// Get reads the named existing guard for exact validation and resourceVersion-based updates; missing state
	// must not be recreated implicitly.
	// Get читает именованный существующий guard для exact validation и resourceVersion-based updates;
	// отсутствующее state нельзя неявно создавать заново.
	Get(context.Context, string, metav1.GetOptions) (*corev1.ConfigMap, error)
	// Update conditionally persists the fetched object's resourceVersion and returns its actual new metadata;
	// ambiguous failure must not authorize encryption.
	// Update условно сохраняет объект с прочитанным resourceVersion и возвращает реальные новые metadata;
	// неопределённый отказ не разрешает шифрование.
	Update(context.Context, *corev1.ConfigMap, metav1.UpdateOptions) (*corev1.ConfigMap, error)
}

// Guard reads durable manifest/use state from one exact ConfigMap for every check or reservation; it keeps
// no local high-watermark and transports no key bytes.
// Guard читает устойчивые manifest/use state из одного exact ConfigMap при каждой проверке и reservation;
// локальный high-watermark не хранит, байты ключей не передаёт.
type Guard struct {
	client          ConfigMaps
	namespace, name string
}

var _ secretdrafts.KeyGuard = (*Guard)(nil)

// keyUse records cumulative encryption reservations for one ID/generation, including allocations whose
// later encryption never occurred.
// keyUse хранит совокупный расход encryption reservations одного ID/generation, включая выделения, после
// которых шифрование не состоялось.
type keyUse struct {
	ID          string `json:"id"`
	Generation  int64  `json:"generation"`
	Encryptions uint64 `json:"encryptions"`
}

// guardState is the strict versioned JSON snapshot with a nullable genesis manifest and nonnil ordered use
// counters; no key material belongs here.
// guardState — strict versioned JSON snapshot с nullable genesis manifest и nonnil ordered use counters;
// материал ключей здесь не хранится.
type guardState struct {
	Version  int                     `json:"v"`
	Manifest *value.DraftKeyManifest `json:"manifest"`
	Uses     []keyUse                `json:"uses"`
}

// New checks nonnil client plus Kubernetes namespace/name syntax without reading or creating the guard;
// trusted composition aligns the namespaced client and supplied namespace.
// New проверяет nonnil client и Kubernetes namespace/name syntax без чтения и создания guard; trusted
// composition согласует namespaced client с переданным namespace.
func New(client ConfigMaps, namespace, name string) (*Guard, error) {
	if client == nil || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, ErrUnavailable
	}
	return &Guard{client: client, namespace: namespace, name: name}, nil
}

// Observe validates/copies manifest key metadata, rejects revision/current-generation rollback or
// retained-key removal, and preserves prior counters while adding new entries. CAS retries re-evaluate
// current durable state; callers keep input stable during the call.
// Observe проверяет/копирует key metadata manifest, отклоняет rollback revision/current-generation и
// удаление retained keys, сохраняя прежние counters при добавлении entries. CAS retries заново проверяют
// durable state; caller сохраняет input стабильным во время вызова.
func (guard *Guard) Observe(ctx context.Context, manifest value.DraftKeyManifest) error {
	if !validManifest(manifest) {
		return ErrUnavailable
	}
	// Не сохраняем caller-owned slice после проверки и не позволяем CAS retry
	// изменять переданный снимок.
	manifest.Keys = append([]value.DraftEncryptionKey(nil), manifest.Keys...)
	return guard.change(ctx, func(state *guardState) (bool, error) {
		if state.Manifest != nil {
			previous := state.Manifest
			if manifest.Revision < previous.Revision || manifest.Current.Generation < previous.Current.Generation {
				return false, ErrUnavailable
			}
			if manifest.Revision == previous.Revision {
				if manifest.Digest != previous.Digest {
					return false, ErrUnavailable
				}
				return false, nil
			}
			accepted := make(map[string]int64, len(manifest.Keys))
			for _, key := range manifest.Keys {
				accepted[key.ID] = key.Generation
			}
			for _, key := range previous.Keys {
				if accepted[key.ID] != key.Generation {
					return false, ErrUnavailable
				}
			}
		}
		uses := make(map[string]keyUse, len(state.Uses))
		for _, used := range state.Uses {
			uses[used.ID] = used
		}
		state.Uses = make([]keyUse, 0, len(manifest.Keys))
		for _, key := range manifest.Keys {
			used, known := uses[key.ID]
			if !known {
				used = keyUse{ID: key.ID, Generation: key.Generation}
			}
			state.Uses = append(state.Uses, used)
		}
		state.Manifest = &manifest
		return true, nil
	})
}

// Reserve conditionally increments exactly the current ID/generation's durable counter below the cap;
// wrong, retired, exhausted or uncertain state refuses use without resetting counters.
// Reserve условно увеличивает устойчивый counter лишь current ID/generation ниже предела; неверное,
// retired, exhausted либо uncertain state запрещает использование без сброса counters.
func (guard *Guard) Reserve(ctx context.Context, key value.DraftEncryptionKey) error {
	if !validKey(key) {
		return ErrUnavailable
	}
	return guard.change(ctx, func(state *guardState) (bool, error) {
		if state.Manifest == nil || state.Manifest.Current != key {
			return false, ErrUnavailable
		}
		for index := range state.Uses {
			used := &state.Uses[index]
			if used.ID == key.ID && used.Generation == key.Generation {
				if used.Encryptions >= MaximumEncryptions {
					return false, ErrUnavailable
				}
				used.Encryptions++
				return true, nil
			}
		}
		return false, ErrUnavailable
	})
}

// CheckCurrent uses the same strict durable state/current-key/budget checks without updating or spending a
// reservation; it is not a check for decrypting retained read keys.
// CheckCurrent применяет те же strict durable state/current-key/budget checks без обновления и расхода
// reservation; это не проверка расшифровки retained read keys.
func (guard *Guard) CheckCurrent(ctx context.Context, key value.DraftEncryptionKey) error {
	if !validKey(key) {
		return ErrUnavailable
	}
	return guard.change(ctx, func(state *guardState) (bool, error) {
		if state.Manifest == nil || state.Manifest.Current != key {
			return false, ErrUnavailable
		}
		for _, used := range state.Uses {
			if used.ID == key.ID && used.Generation == key.Generation && used.Encryptions < MaximumEncryptions {
				return false, nil
			}
		}
		return false, ErrUnavailable
	})
}

// change bounds each operation, refetches and strictly decodes before every mutation, retries only
// explicit CAS conflicts and verifies successful reply UID/new RV/exact serialized state. Unknown update
// outcome returns an error, never resets or recreates state.
// change ограничивает каждую операцию, повторно читает и строго декодирует перед mutation, повторяет лишь
// явные CAS conflicts и сверяет успешный ответ по UID/new RV/exact serialized state. Unknown update
// outcome даёт ошибку без сброса и пересоздания state.
func (guard *Guard) change(ctx context.Context, mutate func(*guardState) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	for attempt := 0; attempt < maximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		object, err := guard.client.Get(ctx, guard.name, metav1.GetOptions{})
		if err != nil {
			return guardError(ctx)
		}
		state, err := guard.decode(object)
		if err != nil {
			return err
		}
		changed, err := mutate(&state)
		if err != nil || !changed {
			return err
		}
		raw, err := json.Marshal(state)
		if err != nil || len(raw) > maximumStateBytes {
			return ErrUnavailable
		}
		next := object.DeepCopy()
		next.Data = map[string]string{StateKey: string(raw)}
		updated, err := guard.client.Update(ctx, next, metav1.UpdateOptions{})
		if apierrors.IsConflict(err) {
			continue
		}
		if err != nil {
			return guardError(ctx)
		}
		if _, err := guard.decode(updated); err != nil || updated.UID != object.UID || updated.ResourceVersion == object.ResourceVersion || updated.Data[StateKey] != string(raw) {
			return ErrUnavailable
		}
		return nil
	}
	return ErrUnavailable
}

// guardError returns the caller's current context error if present, otherwise the fixed guard-unavailable
// sentinel without raw upstream details.
// guardError возвращает текущую context error caller, если она есть, иначе фиксированный guard-unavailable
// sentinel без raw upstream details.
func guardError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrUnavailable
}

// decode requires the exact live mutable managed ConfigMap and sole bounded strict state JSON, accepting
// genesis only with empty counters; populated counters must match ordered manifest identities and stay
// within the cap.
// decode требует exact live mutable managed ConfigMap и единственный bounded strict state JSON, допуская
// genesis лишь с пустыми counters; заполненные counters должны совпадать с ordered manifest identities и
// не превышать предел.
func (guard *Guard) decode(object *corev1.ConfigMap) (guardState, error) {
	var state guardState
	if object == nil || object.Name != guard.name || object.Namespace != guard.namespace || object.UID == "" || object.ResourceVersion == "" || object.DeletionTimestamp != nil || object.Immutable != nil && *object.Immutable || object.Labels[OwnerLabel] != OwnerValue || object.Labels[PurposeLabel] != PurposeValue || len(object.BinaryData) != 0 || len(object.Data) != 1 {
		return state, ErrUnavailable
	}
	raw := object.Data[StateKey]
	if len(raw) == 0 || len(raw) > maximumStateBytes || internalrpcauth.DecodeStrictJSON([]byte(raw), &state) != nil || state.Version != 1 || state.Uses == nil {
		return state, ErrUnavailable
	}
	if state.Manifest == nil {
		if len(state.Uses) != 0 {
			return state, ErrUnavailable
		}
		return state, nil
	}
	if !validManifest(*state.Manifest) || len(state.Uses) != len(state.Manifest.Keys) {
		return state, ErrUnavailable
	}
	for index, key := range state.Manifest.Keys {
		used := state.Uses[index]
		if used.ID != key.ID || used.Generation != key.Generation || used.Encryptions > MaximumEncryptions {
			return state, ErrUnavailable
		}
	}
	return state, nil
}

// InspectRecovery applies the same decoder to the fixed recovery namespace/name and returns a separately
// decoded manifest, possibly nil at genesis, without changing counters or permitting operator edits.
// InspectRecovery применяет тот же decoder к fixed recovery namespace/name и возвращает отдельно decoded
// manifest, возможно nil при genesis, без изменения counters и разрешения operator edits.
func InspectRecovery(object *corev1.ConfigMap) (*value.DraftKeyManifest, error) {
	guard := &Guard{namespace: "kodex-secret-drafts", name: "secret-broker-draft-key-guard"}
	state, err := guard.decode(object)
	if err != nil {
		return nil, err
	}
	return state.Manifest, nil
}

// validManifest checks bounded canonical key identities, strictly increasing generations/current maximum
// and exact digest over the material-free manifest with Digest cleared; revision gaps are allowed.
// validManifest проверяет bounded canonical key identities, строго возрастающие generations/current
// maximum и exact digest manifest без материала с очищенным Digest; разрывы revision допустимы.
func validManifest(manifest value.DraftKeyManifest) bool {
	if manifest.Revision < 1 || len(manifest.Keys) == 0 || len(manifest.Keys) > maximumManifestKeys || !validKey(manifest.Current) || !validDigest(manifest.Digest) {
		return false
	}
	seen := make(map[string]bool, len(manifest.Keys))
	var last int64
	for _, key := range manifest.Keys {
		if !validKey(key) || seen[key.ID] || key.Generation <= last || key.Generation > manifest.Revision {
			return false
		}
		seen[key.ID] = true
		last = key.Generation
	}
	if manifest.Current != manifest.Keys[len(manifest.Keys)-1] {
		return false
	}
	digest := manifest.Digest
	manifest.Digest = ""
	raw, err := json.Marshal(manifest)
	if err != nil {
		return false
	}
	computed := sha256.Sum256(raw)
	return hex.EncodeToString(computed[:]) == digest
}

// validKey requires a positive generation and canonical SHA-256-shaped ID; it does not possess or hash key
// material.
// validKey требует положительный generation и canonical SHA-256-shaped ID; материалом ключа не располагает
// и не хеширует его.
func validKey(key value.DraftEncryptionKey) bool { return key.Generation > 0 && validDigest(key.ID) }

// validDigest accepts exactly 64 lowercase hexadecimal characters, including all zeros; format alone does
// not prove correspondence to another object.
// validDigest принимает ровно 64 lowercase hexadecimal символа, включая все нули; формат не доказывает
// соответствия иному объекту.
func validDigest(digest string) bool {
	if len(digest) != 64 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
