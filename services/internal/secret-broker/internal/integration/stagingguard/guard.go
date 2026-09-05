// Package stagingguard хранит поколения ключей и бюджет шифрования в отдельном
// ConfigMap. Материал ключей не проходит через эту границу.
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
	MaximumEncryptions  uint64 = 1 << 24
	StateKey                   = "state.json"
	OwnerLabel                 = "app.kubernetes.io/managed-by"
	OwnerValue                 = "kodex-secret-broker-bootstrap"
	PurposeLabel               = "kodex.dev/purpose"
	PurposeValue               = "secret-draft-key-guard"
	maximumManifestKeys        = 128
	maximumStateBytes          = 64 << 10
	maximumAttempts            = 8
	operationTimeout           = 5 * time.Second
)

var ErrUnavailable = errors.New("secret draft key guard is unavailable")

// ConfigMaps ограничивает adapter двумя операциями над существующим объектом.
// Namespace выбирается typed Kubernetes client при создании adapter.
type ConfigMaps interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.ConfigMap, error)
	Update(context.Context, *corev1.ConfigMap, metav1.UpdateOptions) (*corev1.ConfigMap, error)
}

type Guard struct {
	client          ConfigMaps
	namespace, name string
}

var _ secretdrafts.KeyGuard = (*Guard)(nil)

type keyUse struct {
	ID          string `json:"id"`
	Generation  int64  `json:"generation"`
	Encryptions uint64 `json:"encryptions"`
}
type guardState struct {
	Version  int                     `json:"v"`
	Manifest *value.DraftKeyManifest `json:"manifest"`
	Uses     []keyUse                `json:"uses"`
}

func New(client ConfigMaps, namespace, name string) (*Guard, error) {
	if client == nil || len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return nil, ErrUnavailable
	}
	return &Guard{client: client, namespace: namespace, name: name}, nil
}

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

// CheckCurrent проверяет тот же устойчивый лимит, не расходуя nonce budget
// на readiness. Исчерпание write key не запрещает Resolve retained read key.
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

func guardError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrUnavailable
}

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

func validKey(key value.DraftEncryptionKey) bool { return key.Generation > 0 && validDigest(key.ID) }
func validDigest(digest string) bool {
	if len(digest) != 64 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
