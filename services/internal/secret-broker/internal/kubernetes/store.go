// Package kubernetes хранит только immutable versioned Secret, которыми владеет secret-broker.
package kubernetes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
)

const (
	managedLabel              = "runtime-secrets.kodex.dev/managed"
	readinessSecretName       = "runtime-secret-readiness-probe"
	operationRefAnnotation    = "runtime-secrets.kodex.dev/operation-ref"
	claimGenerationAnnotation = "runtime-secrets.kodex.dev/claim-generation"
	secretRefAnnotation       = "runtime-secrets.kodex.dev/secret-ref"
	secretKeyAnnotation       = "runtime-secrets.kodex.dev/secret-key"
	revisionAnnotation        = "runtime-secrets.kodex.dev/revision"
	digestAnnotation          = "runtime-secrets.kodex.dev/content-sha256"
)

var (
	ErrMaterializationNotFound          = errors.New("runtime secret materialization is not found")
	ErrMaterializationConflict          = errors.New("runtime secret materialization conflicts with expected effect")
	ErrMaterializationInvalid           = errors.New("runtime secret materialization input is invalid")
	ErrExactDeletePreconditionsRequired = errors.New("exact runtime secret materialization is required for deletion")
)

// MaterializationEffect связывает внешний эффект с одной fenced claim attempt.
// ContentSHA256 является частью авторизованного intent и проверяется до записи.
type MaterializationEffect struct {
	OperationRef    string
	ClaimGeneration int64
	SecretRef       string
	Key             string
	Revision        int64
	ContentSHA256   string
}

// Materialization содержит только идентичность и metadata объекта. Значение
// Secret намеренно отсутствует, поэтому этот тип безопасен для recovery worker.
type Materialization struct {
	Namespace       string
	Name            string
	OperationRef    string
	ClaimGeneration int64
	SecretRef       string
	Key             string
	Revision        int64
	UID             string
	ResourceVersion string
	ContentSHA256   string
}

// ExactDescriptor содержит назначенную control-plane идентичность revision.
// OperationRef и ClaimGeneration проверяются по metadata объекта, но не
// принимаются от reveal/revoke caller: они принадлежат исходной materialization.
type ExactDescriptor struct {
	Namespace       string
	Name            string
	SecretRef       string
	Key             string
	Revision        int64
	UID             string
	ResourceVersion string
	ContentSHA256   string
}

// RecoveryStore описывает API, достаточный для поиска, exact readback и
// безопасного удаления materialization без возврата plaintext.
type RecoveryStore interface {
	LookupExpectedEffect(context.Context, MaterializationEffect) (Materialization, error)
	ListManaged(context.Context) ([]Materialization, error)
	ReadbackExact(context.Context, Materialization) (Materialization, error)
	DeleteExact(context.Context, Materialization) error
}

type Store struct {
	client    kubernetes.Interface
	namespace string
}

func InCluster(namespace string) (*Store, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load Kubernetes secret broker configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, errors.New("create Kubernetes secret broker client")
	}
	return New(client, namespace)
}

func New(client kubernetes.Interface, namespace string) (*Store, error) {
	if client == nil || namespace == "" {
		return nil, errors.New("Kubernetes secret store configuration is invalid")
	}
	return &Store{client: client, namespace: namespace}, nil
}

func (store *Store) Namespace() string { return store.namespace }

func (store *Store) Check(ctx context.Context) error {
	_, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, readinessSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("Kubernetes Secret API is unavailable")
	}
	return nil
}

// CreateImmutableForEffect идемпотентен только для того же operation, claim
// generation, secret revision, key и content digest.
func (store *Store) CreateImmutableForEffect(ctx context.Context, effect MaterializationEffect, value []byte) (Materialization, error) {
	name, err := validateEffectValue(effect, value)
	if err != nil {
		return Materialization{}, err
	}
	immutable := true
	wanted := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: store.namespace,
			Labels:    map[string]string{managedLabel: "true"},
			Annotations: map[string]string{
				operationRefAnnotation:    effect.OperationRef,
				claimGenerationAnnotation: strconv.FormatInt(effect.ClaimGeneration, 10),
				secretRefAnnotation:       effect.SecretRef,
				secretKeyAnnotation:       effect.Key,
				revisionAnnotation:        strconv.FormatInt(effect.Revision, 10),
				digestAnnotation:          effect.ContentSHA256,
			},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data:      map[string][]byte{effect.Key: append([]byte(nil), value...)},
	}
	defer clearSecretData(wanted)
	created, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return Materialization{}, errors.New("create immutable Kubernetes Secret")
	}
	defer clearSecretData(created)
	materialized, err := materializationFromSecret(created, true)
	if err != nil || !materializationMatchesEffect(materialized, effect) {
		return Materialization{}, ErrMaterializationConflict
	}
	return materialized, nil
}

// LookupExpectedEffect проверяет deterministic materialization просроченной
// claim без возврата plaintext. Фактическое значение используется только для
// проверки digest и обнуляется в полученном Kubernetes DTO.
func (store *Store) LookupExpectedEffect(ctx context.Context, effect MaterializationEffect) (Materialization, error) {
	name, err := validateEffectMetadata(effect)
	if err != nil {
		return Materialization{}, err
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Materialization{}, ErrMaterializationNotFound
	}
	if err != nil {
		return Materialization{}, errors.New("look up runtime secret materialization effect")
	}
	defer clearSecretData(secret)
	materialized, err := materializationFromSecret(secret, true)
	if err != nil || !materializationMatchesEffect(materialized, effect) {
		return Materialization{}, ErrMaterializationConflict
	}
	return materialized, nil
}

// ListManaged возвращает только metadata управляемых immutable Secret. Data не
// читается и не переносится в возвращаемые структуры.
func (store *Store) ListManaged(ctx context.Context) ([]Materialization, error) {
	selector := labels.Set{managedLabel: "true"}.AsSelector().String()
	list, err := store.client.CoreV1().Secrets(store.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, errors.New("list managed runtime secret materializations")
	}
	result := make([]Materialization, 0, len(list.Items))
	for index := range list.Items {
		secret := &list.Items[index]
		materialized, parseErr := materializationFromSecret(secret, false)
		clearSecretData(secret)
		if parseErr != nil {
			return nil, fmt.Errorf("list managed runtime secret materializations: %w", parseErr)
		}
		result = append(result, materialized)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].SecretRef == result[right].SecretRef {
			if result[left].Revision == result[right].Revision {
				return result[left].Name < result[right].Name
			}
			return result[left].Revision < result[right].Revision
		}
		return result[left].SecretRef < result[right].SecretRef
	})
	return result, nil
}

// ReadbackExact повторно читает объект и подтверждает metadata, UID,
// resourceVersion и фактический digest, не возвращая plaintext.
func (store *Store) ReadbackExact(ctx context.Context, expected Materialization) (Materialization, error) {
	if err := validateExactMaterialization(expected, store.namespace); err != nil {
		return Materialization{}, err
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Materialization{}, ErrMaterializationNotFound
	}
	if err != nil {
		return Materialization{}, errors.New("read back runtime secret materialization")
	}
	defer clearSecretData(secret)
	actual, err := materializationFromSecret(secret, true)
	if err != nil || !sameMaterialization(actual, expected) {
		return Materialization{}, ErrMaterializationConflict
	}
	return actual, nil
}

// DeleteExact удаляет только ранее прочитанную exact materialization. Проверка
// перед Delete даёт понятный fail-closed ответ, а UID/RV preconditions закрывают
// гонку между readback и удалением на Kubernetes API server.
func (store *Store) DeleteExact(ctx context.Context, expected Materialization) error {
	if _, err := store.ReadbackExact(ctx, expected); err != nil {
		if errors.Is(err, ErrMaterializationNotFound) {
			return nil
		}
		return err
	}
	uid := types.UID(expected.UID)
	resourceVersion := expected.ResourceVersion
	err := store.client.CoreV1().Secrets(store.namespace).Delete(ctx, expected.Name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion},
	})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if apierrors.IsConflict(err) || apierrors.IsInvalid(err) {
		return ErrMaterializationConflict
	}
	if err != nil {
		return errors.New("delete exact runtime secret materialization")
	}
	readback, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read back exact runtime secret deletion")
	}
	clearSecretData(readback)
	return ErrMaterializationConflict
}

// ResolveExact проверяет назначенный control-plane descriptor и возвращает
// фактическую identity materialization, включая её operation metadata.
func (store *Store) ResolveExact(ctx context.Context, expected ExactDescriptor) (Materialization, error) {
	materialized, _, err := store.readExact(ctx, expected, false)
	return materialized, err
}

// ReadExactValue возвращает plaintext только после exact проверки namespace,
// name, UID, resourceVersion, revision и digest. Копия внутри Kubernetes DTO
// обнуляется до возврата из adapter.
func (store *Store) ReadExactValue(ctx context.Context, expected ExactDescriptor) (Materialization, []byte, error) {
	return store.readExact(ctx, expected, true)
}

func (store *Store) readExact(ctx context.Context, expected ExactDescriptor, returnValue bool) (Materialization, []byte, error) {
	if err := validateExactDescriptor(expected, store.namespace); err != nil {
		return Materialization{}, nil, err
	}
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Materialization{}, nil, ErrMaterializationNotFound
	}
	if err != nil {
		return Materialization{}, nil, errors.New("read exact runtime secret materialization")
	}
	defer clearSecretData(secret)
	materialized, err := materializationFromSecret(secret, true)
	if err != nil || !materializationMatchesDescriptor(materialized, expected) {
		return Materialization{}, nil, ErrMaterializationConflict
	}
	if !returnValue {
		return materialized, nil, nil
	}
	value := append([]byte(nil), secret.Data[expected.Key]...)
	return materialized, value, nil
}

func validateEffectValue(effect MaterializationEffect, value []byte) (string, error) {
	name, err := validateEffectMetadata(effect)
	if err != nil || len(value) == 0 {
		return "", ErrMaterializationInvalid
	}
	digest := sha256.Sum256(value)
	if subtle.ConstantTimeCompare([]byte(effect.ContentSHA256), []byte(hex.EncodeToString(digest[:]))) != 1 {
		return "", ErrMaterializationInvalid
	}
	return name, nil
}

func validateEffectMetadata(effect MaterializationEffect) (string, error) {
	if effect.OperationRef == "" || effect.ClaimGeneration < 1 || effect.SecretRef == "" ||
		effect.Key == "" || effect.Revision < 1 || !validDigest(effect.ContentSHA256) {
		return "", ErrMaterializationInvalid
	}
	name, err := runtimesecret.VersionedKubernetesName(effect.SecretRef, effect.Revision)
	if err != nil {
		return "", ErrMaterializationInvalid
	}
	return name, nil
}

func clearSecretData(secret *corev1.Secret) {
	if secret == nil {
		return
	}
	for key := range secret.Data {
		clear(secret.Data[key])
	}
}

func materializationFromSecret(secret *corev1.Secret, verifyContent bool) (Materialization, error) {
	if secret == nil || secret.Namespace == "" || secret.Name == "" || secret.UID == "" || secret.ResourceVersion == "" ||
		secret.Labels[managedLabel] != "true" || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque {
		return Materialization{}, errors.New("runtime secret materialization metadata is invalid")
	}
	claimGeneration, err := strconv.ParseInt(secret.Annotations[claimGenerationAnnotation], 10, 64)
	if err != nil || claimGeneration < 1 {
		return Materialization{}, errors.New("runtime secret claim generation is invalid")
	}
	revision, err := strconv.ParseInt(secret.Annotations[revisionAnnotation], 10, 64)
	if err != nil || revision < 1 {
		return Materialization{}, errors.New("runtime secret revision is invalid")
	}
	materialized := Materialization{
		Namespace:       secret.Namespace,
		Name:            secret.Name,
		OperationRef:    secret.Annotations[operationRefAnnotation],
		ClaimGeneration: claimGeneration,
		SecretRef:       secret.Annotations[secretRefAnnotation],
		Key:             secret.Annotations[secretKeyAnnotation],
		Revision:        revision,
		UID:             string(secret.UID),
		ResourceVersion: secret.ResourceVersion,
		ContentSHA256:   secret.Annotations[digestAnnotation],
	}
	if materialized.OperationRef == "" || materialized.SecretRef == "" || materialized.Key == "" || !validDigest(materialized.ContentSHA256) {
		return Materialization{}, errors.New("runtime secret materialization annotations are invalid")
	}
	expectedName, err := runtimesecret.VersionedKubernetesName(materialized.SecretRef, materialized.Revision)
	if err != nil || expectedName != materialized.Name {
		return Materialization{}, errors.New("runtime secret materialization name is invalid")
	}
	if verifyContent {
		value, exists := secret.Data[materialized.Key]
		if !exists || len(secret.Data) != 1 {
			return Materialization{}, errors.New("runtime secret materialization data is invalid")
		}
		digest := sha256.Sum256(value)
		if subtle.ConstantTimeCompare([]byte(materialized.ContentSHA256), []byte(hex.EncodeToString(digest[:]))) != 1 {
			return Materialization{}, errors.New("runtime secret materialization digest is invalid")
		}
	}
	return materialized, nil
}

func validateExactMaterialization(materialized Materialization, namespace string) error {
	if materialized.Namespace != namespace || materialized.Name == "" || materialized.OperationRef == "" ||
		materialized.ClaimGeneration < 1 || materialized.SecretRef == "" || materialized.Key == "" ||
		materialized.Revision < 1 || materialized.UID == "" || materialized.ResourceVersion == "" ||
		!validDigest(materialized.ContentSHA256) {
		return ErrExactDeletePreconditionsRequired
	}
	expectedName, err := runtimesecret.VersionedKubernetesName(materialized.SecretRef, materialized.Revision)
	if err != nil || expectedName != materialized.Name {
		return ErrExactDeletePreconditionsRequired
	}
	return nil
}

func validateExactDescriptor(descriptor ExactDescriptor, namespace string) error {
	if descriptor.Namespace != namespace || descriptor.Name == "" || descriptor.SecretRef == "" || descriptor.Key == "" ||
		descriptor.Revision < 1 || descriptor.UID == "" || descriptor.ResourceVersion == "" || !validDigest(descriptor.ContentSHA256) {
		return ErrExactDeletePreconditionsRequired
	}
	expectedName, err := runtimesecret.VersionedKubernetesName(descriptor.SecretRef, descriptor.Revision)
	if err != nil || expectedName != descriptor.Name {
		return ErrExactDeletePreconditionsRequired
	}
	return nil
}

func materializationMatchesEffect(materialized Materialization, effect MaterializationEffect) bool {
	return materialized.OperationRef == effect.OperationRef &&
		materialized.ClaimGeneration == effect.ClaimGeneration &&
		materialized.SecretRef == effect.SecretRef &&
		materialized.Key == effect.Key &&
		materialized.Revision == effect.Revision &&
		subtle.ConstantTimeCompare([]byte(materialized.ContentSHA256), []byte(effect.ContentSHA256)) == 1
}

func sameMaterialization(actual, expected Materialization) bool {
	return actual.Namespace == expected.Namespace && actual.Name == expected.Name &&
		actual.OperationRef == expected.OperationRef && actual.ClaimGeneration == expected.ClaimGeneration &&
		actual.SecretRef == expected.SecretRef && actual.Key == expected.Key && actual.Revision == expected.Revision &&
		actual.UID == expected.UID && actual.ResourceVersion == expected.ResourceVersion &&
		subtle.ConstantTimeCompare([]byte(actual.ContentSHA256), []byte(expected.ContentSHA256)) == 1
}

func materializationMatchesDescriptor(actual Materialization, expected ExactDescriptor) bool {
	return actual.Namespace == expected.Namespace && actual.Name == expected.Name &&
		actual.SecretRef == expected.SecretRef && actual.Key == expected.Key && actual.Revision == expected.Revision &&
		actual.UID == expected.UID && actual.ResourceVersion == expected.ResourceVersion &&
		subtle.ConstantTimeCompare([]byte(actual.ContentSHA256), []byte(expected.ContentSHA256)) == 1
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
