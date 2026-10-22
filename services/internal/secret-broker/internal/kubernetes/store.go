// Package kubernetes stores immutable versioned runtime secrets and exact credential projections;
// domain authorization is supplied by the owning services above the Kubernetes adapter.
// Package kubernetes хранит immutable версии runtime-секретов и точные проекции учётных данных;
// доменную авторизацию обеспечивают сервисы-владельцы выше Kubernetes-адаптера.
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
	// managedLabel selects immutable runtime-secret objects owned by this adapter; the label alone does not
	// establish an exact authorized effect.
	// managedLabel выбирает immutable runtime-secret объекты этого адаптера; сама метка не доказывает точный
	// разрешённый эффект.
	managedLabel = "runtime-secrets.kodex.dev/managed"
	// workKindAnnotation persists the explicit owner route so recovery never infers draft or immediate
	// ownership from a reference prefix.
	// workKindAnnotation сохраняет явный маршрут владельца, чтобы recovery не выводил принадлежность draft
	// или immediate из префикса ссылки.
	workKindAnnotation = "runtime-secrets.kodex.dev/work-kind"
	// readinessSecretName is the fixed probe target; a missing object still proves the Secret GET API is
	// reachable without creating a probe.
	// readinessSecretName задаёт фиксированную цель пробы; отсутствие объекта допускается и проверяет
	// доступность Secret GET без создания пробы.
	readinessSecretName = "runtime-secret-readiness-probe"
	// operationRefAnnotation binds the immutable object to the operation that caused its materialization.
	// operationRefAnnotation связывает immutable объект с операцией, вызвавшей его материализацию.
	operationRefAnnotation = "runtime-secrets.kodex.dev/operation-ref"
	// claimGenerationAnnotation records the positive claim generation used to fence retries of a
	// materialization effect.
	// claimGenerationAnnotation записывает положительное поколение claim для разграничения повторов эффекта
	// материализации.
	claimGenerationAnnotation = "runtime-secrets.kodex.dev/claim-generation"
	// secretRefAnnotation records the domain secret reference used together with revision to validate the
	// deterministic object name.
	// secretRefAnnotation записывает доменную ссылку секрета, которая вместе с revision проверяет
	// детерминированное имя объекта.
	secretRefAnnotation = "runtime-secrets.kodex.dev/secret-ref"
	// secretKeyAnnotation identifies the single data entry that must exist when content verification is
	// requested.
	// secretKeyAnnotation указывает единственный элемент Data, наличие которого обязательно при проверке
	// содержимого.
	secretKeyAnnotation = "runtime-secrets.kodex.dev/secret-key"
	// revisionAnnotation pins a positive secret revision rather than a mutable latest-version alias.
	// revisionAnnotation закрепляет положительную ревизию секрета вместо изменяемого указателя на последнюю
	// версию.
	revisionAnnotation = "runtime-secrets.kodex.dev/revision"
	// digestAnnotation stores the lowercase SHA-256 expected for the exact secret bytes; verified reads
	// recompute it before returning metadata or a copy.
	// digestAnnotation хранит ожидаемый lowercase SHA-256 точных байтов секрета; проверяемое чтение
	// пересчитывает его до возврата metadata или копии.
	digestAnnotation = "runtime-secrets.kodex.dev/content-sha256"
)

var (
	// ErrMaterializationNotFound distinguishes authoritative API absence from malformed content, stale
	// identity and transport failure.
	// ErrMaterializationNotFound отличает подтверждённое API отсутствие от повреждённого содержимого,
	// устаревшей identity и ошибки транспорта.
	ErrMaterializationNotFound = errors.New("runtime secret materialization is not found")
	// ErrMaterializationConflict reports an existing object or delete precondition that does not match the
	// expected immutable effect.
	// ErrMaterializationConflict сообщает о существующем объекте или предусловии удаления, не совпадающем с
	// ожидаемым immutable эффектом.
	ErrMaterializationConflict = errors.New("runtime secret materialization conflicts with expected effect")
	// ErrMaterializationInvalid rejects incomplete effect metadata or bytes whose digest differs before any
	// Kubernetes create request.
	// ErrMaterializationInvalid отклоняет неполные metadata эффекта или байты с другим digest до запроса
	// создания в Kubernetes.
	ErrMaterializationInvalid = errors.New("runtime secret materialization input is invalid")
	// ErrExactDeletePreconditionsRequired rejects incomplete exact materialization or descriptor identities
	// before reads used for deletion or reveal.
	// ErrExactDeletePreconditionsRequired отклоняет неполную точную identity materialization или descriptor
	// до чтений для удаления или раскрытия.
	ErrExactDeletePreconditionsRequired = errors.New("exact runtime secret materialization is required for deletion")
)

// MaterializationEffect carries the owner route, operation, claim generation and secret revision intent;
// its content digest is checked against bytes before writing. Callers must obtain authority before
// constructing it.
// MaterializationEffect несёт маршрут владельца, операцию, поколение claim и намерение записи ревизии
// секрета; digest проверяется по байтам до записи. Полномочия вызывающая сторона получает до его
// построения.
type MaterializationEffect struct {
	WorkKind        MaterializationWorkKind
	OperationRef    string
	ClaimGeneration int64
	SecretRef       string
	Key             string
	Revision        int64
	ContentSHA256   string
}

// Materialization returns object identity and fenced operation metadata without plaintext, allowing
// recovery to compare and delete one exact external effect.
// Materialization возвращает identity объекта и metadata операции с поколением claim без plaintext,
// позволяя recovery сравнить и удалить один точный внешний эффект.
type Materialization struct {
	WorkKind        MaterializationWorkKind
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

// ExactDescriptor pins the control-plane-selected object, key, revision, UID, resourceVersion and
// digest. Original operation and claim metadata are recovered from the object, not accepted from a
// reveal or revoke caller.
// ExactDescriptor закрепляет выбранные control-plane объект, ключ, revision, UID, resourceVersion и
// digest. Исходные operation и claim metadata читаются из объекта, а не принимаются от вызывающего
// reveal или revoke.
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

// RecoveryStore limits recovery to exact effect lookup, metadata listing, verified readback and
// preconditioned deletion; no method returns plaintext.
// RecoveryStore ограничивает recovery поиском точного эффекта, списком metadata, проверяемым чтением и
// удалением с предусловиями; ни один метод не возвращает plaintext.
type RecoveryStore interface {
	// LookupExpectedEffect locates the deterministic object for one fenced intent and distinguishes absence
	// from a mismatched effect without returning its value.
	// LookupExpectedEffect находит детерминированный объект одного намерения с поколением claim и отличает
	// отсутствие от несовпадения эффекта без возврата значения.
	LookupExpectedEffect(context.Context, MaterializationEffect) (Materialization, error)
	// ListManaged enumerates managed materialization metadata for reconciliation; implementations must
	// reject malformed managed objects instead of silently dropping them.
	// ListManaged перечисляет metadata управляемых materialization для сверки; реализации должны отклонять
	// повреждённые управляемые объекты, а не молча пропускать их.
	ListManaged(context.Context) ([]Materialization, error)
	// ReadbackExact verifies one full object identity and content digest before recovery treats the external
	// effect as the expected materialization.
	// ReadbackExact проверяет полную identity объекта и digest содержимого до признания внешнего эффекта
	// ожидаемой materialization.
	ReadbackExact(context.Context, Materialization) (Materialization, error)
	// DeleteExact removes only the previously read materialization with UID and resourceVersion
	// preconditions; already absent objects are idempotent success.
	// DeleteExact удаляет только ранее прочитанную materialization с предусловиями UID и resourceVersion;
	// отсутствие объекта считается идемпотентным успехом.
	DeleteExact(context.Context, Materialization) error
}

// Store confines Kubernetes Secret operations to one configured namespace; domain authority and claim
// ownership are checked above this storage adapter.
// Store ограничивает операции Kubernetes Secret одним настроенным namespace; доменные полномочия и
// владение claim проверяются выше этого адаптера хранения.
type Store struct {
	client    kubernetes.Interface
	namespace string
}

// InCluster builds a client from the Pod service account with a five-second request timeout, then
// validates the namespace through New; it does not perform a readiness request.
// InCluster строит клиент по service account Pod с таймаутом запросов пять секунд, затем проверяет
// namespace через New; readiness-запрос здесь не выполняется.
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

// New rejects a nil client or empty namespace and stores the supplied client without connecting to
// Kubernetes or validating namespace existence.
// New отклоняет nil клиент или пустой namespace и сохраняет переданный клиент без соединения с
// Kubernetes и проверки существования namespace.
func New(client kubernetes.Interface, namespace string) (*Store, error) {
	if client == nil || namespace == "" {
		return nil, errors.New("Kubernetes secret store configuration is invalid")
	}
	return &Store{client: client, namespace: namespace}, nil
}

// Namespace returns the adapter's configured namespace without reading Kubernetes or deriving caller
// scope.
// Namespace возвращает настроенный namespace адаптера без чтения Kubernetes и определения scope
// вызывающей стороны.
func (store *Store) Namespace() string { return store.namespace }

// Check issues a GET for the fixed readiness object and accepts NotFound; other API errors fail
// readiness without exposing upstream details or writing any object.
// Check выполняет GET фиксированного readiness-объекта и допускает NotFound; другие ошибки API закрывают
// готовность без раскрытия деталей upstream и записи объектов.
func (store *Store) Check(ctx context.Context) error {
	_, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, readinessSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("Kubernetes Secret API is unavailable")
	}
	return nil
}

// CreateImmutableForEffect validates the bytes and fenced intent, then creates an immutable single-key
// Secret. AlreadyExists triggers exact readback rather than overwrite; any different effect conflicts.
// Temporary request and response data copies are cleared.
// CreateImmutableForEffect проверяет байты и намерение с поколением claim, затем создаёт immutable
// Secret с одним ключом. AlreadyExists вызывает точное чтение вместо перезаписи; другой эффект даёт
// конфликт. Временные копии Data запроса и ответа обнуляются.
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
				workKindAnnotation:        string(effect.WorkKind),
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

// LookupExpectedEffect validates the intent, reads its deterministic name and verifies the actual value
// digest and all effect metadata. It returns no plaintext and clears the fetched DTO data.
// LookupExpectedEffect проверяет намерение, читает его детерминированное имя и сверяет digest
// фактического значения со всеми metadata эффекта. Plaintext не возвращается, Data полученного DTO
// обнуляется.
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

// ListManaged requests managed-label Secrets, validates metadata without hashing Data and sorts by
// secret reference, revision and name. Kubernetes still returns full Secret DTOs; processed data buffers
// are cleared, and a malformed item aborts the scan.
// ListManaged запрашивает Secret с managed-меткой, проверяет metadata без хеширования Data и сортирует
// по ссылке секрета, revision и имени. Kubernetes всё равно возвращает полные DTO Secret; обработанные
// буферы Data обнуляются, повреждённый элемент прерывает обход.
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

// ReadbackExact validates the namespace and complete expected identity, rereads the object and checks
// its bytes and all metadata; absence and mismatch remain distinct and plaintext never escapes.
// ReadbackExact проверяет namespace и полную ожидаемую identity, перечитывает объект и сверяет его байты
// со всеми metadata; отсутствие отличается от несовпадения, plaintext наружу не выходит.
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

// DeleteExact first verifies the complete materialization, then requests deletion with UID and
// resourceVersion preconditions and requires absence readback. Missing objects are idempotent success;
// stale identities and surviving objects conflict.
// DeleteExact сначала проверяет полную materialization, затем запрашивает удаление с предусловиями UID и
// resourceVersion и требует чтения отсутствия. Отсутствие идемпотентно; устаревшие identity и
// сохранившиеся объекты дают конфликт.
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

// ResolveExact verifies the control-plane descriptor against actual bytes and returns the original
// operation metadata without copying plaintext to the caller.
// ResolveExact сверяет descriptor control-plane с фактическими байтами и возвращает исходные metadata
// операции без копирования plaintext вызывающему.
func (store *Store) ResolveExact(ctx context.Context, expected ExactDescriptor) (Materialization, error) {
	materialized, _, err := store.readExact(ctx, expected, false)
	return materialized, err
}

// ReadExactValue returns an independent plaintext copy only after exact descriptor and content
// verification; the Kubernetes DTO copy is cleared and the caller owns clearing the returned bytes.
// ReadExactValue возвращает отдельную копию plaintext только после проверки точного descriptor и
// содержимого; копия в DTO Kubernetes обнуляется, возвращённые байты обязан обнулить вызывающий.
func (store *Store) ReadExactValue(ctx context.Context, expected ExactDescriptor) (Materialization, []byte, error) {
	return store.readExact(ctx, expected, true)
}

// readExact shares descriptor validation, API error mapping and integrity checks for metadata-only and
// value reads; returnValue controls only the final byte copy, not verification.
// readExact объединяет проверку descriptor, преобразование ошибок API и контроль целостности для чтения
// metadata и значения; returnValue управляет только итоговым копированием байтов, а не проверкой.
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

// validateEffectValue validates intent metadata and nonempty bytes, then compares their SHA-256 with the
// expected lowercase digest before returning the deterministic name.
// validateEffectValue проверяет metadata намерения и непустые байты, затем сравнивает их SHA-256 с
// ожидаемым lowercase digest до возврата детерминированного имени.
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

// validateEffectMetadata requires an explicit known owner route, operation reference, positive claim and
// revision, key and digest, then derives the versioned name; it does not establish domain authorization.
// validateEffectMetadata требует явный известный маршрут владельца, ссылку операции, положительные claim
// и revision, ключ и digest, затем выводит имя версии; доменная авторизация здесь не устанавливается.
func validateEffectMetadata(effect MaterializationEffect) (string, error) {
	if !effect.WorkKind.Valid() || effect.OperationRef == "" || effect.ClaimGeneration < 1 || effect.SecretRef == "" ||
		effect.Key == "" || effect.Revision < 1 || !validDigest(effect.ContentSHA256) {
		return "", ErrMaterializationInvalid
	}
	name, err := runtimesecret.VersionedKubernetesName(effect.SecretRef, effect.Revision)
	if err != nil {
		return "", ErrMaterializationInvalid
	}
	return name, nil
}

// clearSecretData zeroes each mutable Data byte slice of a nonnil Secret; it leaves map entries and
// unrelated copies unchanged.
// clearSecretData обнуляет каждый изменяемый байтовый срез Data у ненулевого Secret; элементы map и
// посторонние копии не изменяются.
func clearSecretData(secret *corev1.Secret) {
	if secret == nil {
		return
	}
	for key := range secret.Data {
		clear(secret.Data[key])
	}
}

// materializationFromSecret validates managed immutable metadata, explicit work kind and deterministic
// revision name. With verifyContent it additionally requires exactly one declared data key and a
// matching SHA-256; it never returns plaintext.
// materializationFromSecret проверяет управляемые immutable metadata, явный work kind и
// детерминированное имя ревизии. При verifyContent дополнительно требует ровно один заявленный ключ Data
// и совпадающий SHA-256; plaintext не возвращается.
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
		WorkKind:        MaterializationWorkKind(secret.Annotations[workKindAnnotation]),
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
	if !materialized.WorkKind.Valid() || materialized.OperationRef == "" || materialized.SecretRef == "" || materialized.Key == "" || !validDigest(materialized.ContentSHA256) {
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

// validateExactMaterialization requires the configured namespace, explicit owner route, full operation
// fence and exact object pins including deterministic name; it validates shape, not current Kubernetes
// existence.
// validateExactMaterialization требует настроенный namespace, явный маршрут владельца, полные operation
// и claim generation и точные привязки объекта с детерминированным именем; проверяется форма, не текущее
// существование в Kubernetes.
func validateExactMaterialization(materialized Materialization, namespace string) error {
	if !materialized.WorkKind.Valid() || materialized.Namespace != namespace || materialized.Name == "" || materialized.OperationRef == "" ||
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

// validateExactDescriptor rejects missing exact read pins or a name inconsistent with secret reference
// and revision; domain ownership remains the caller's prerequisite.
// validateExactDescriptor отклоняет неполные точные привязки чтения и имя, не согласованное со ссылкой
// секрета и revision; проверка доменного владения остаётся предварительным условием вызывающего.
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

// materializationMatchesEffect compares owner route, operation fence, secret key and revision with the
// expected intent, using constant-time digest comparison; it does not compare object UID or
// resourceVersion.
// materializationMatchesEffect сравнивает маршрут владельца, operation и claim generation, ключ и
// revision секрета с намерением, используя constant-time сравнение digest; UID и resourceVersion объекта
// здесь не сравниваются.
func materializationMatchesEffect(materialized Materialization, effect MaterializationEffect) bool {
	return materialized.WorkKind == effect.WorkKind && materialized.OperationRef == effect.OperationRef &&
		materialized.ClaimGeneration == effect.ClaimGeneration &&
		materialized.SecretRef == effect.SecretRef &&
		materialized.Key == effect.Key &&
		materialized.Revision == effect.Revision &&
		subtle.ConstantTimeCompare([]byte(materialized.ContentSHA256), []byte(effect.ContentSHA256)) == 1
}

// sameMaterialization compares every returned identity and operation field, including UID and
// resourceVersion, with constant-time digest comparison for exact readback.
// sameMaterialization сравнивает все возвращённые поля identity и операции, включая UID и
// resourceVersion, с constant-time сравнением digest для точного чтения.
func sameMaterialization(actual, expected Materialization) bool {
	return actual.WorkKind == expected.WorkKind && actual.Namespace == expected.Namespace && actual.Name == expected.Name &&
		actual.OperationRef == expected.OperationRef && actual.ClaimGeneration == expected.ClaimGeneration &&
		actual.SecretRef == expected.SecretRef && actual.Key == expected.Key && actual.Revision == expected.Revision &&
		actual.UID == expected.UID && actual.ResourceVersion == expected.ResourceVersion &&
		subtle.ConstantTimeCompare([]byte(actual.ContentSHA256), []byte(expected.ContentSHA256)) == 1
}

// materializationMatchesDescriptor compares only the control-plane descriptor pins; operation and claim
// metadata are separately validated when parsing the actual object.
// materializationMatchesDescriptor сравнивает только привязки descriptor control-plane; metadata
// операции и claim отдельно проверяются при разборе фактического объекта.
func materializationMatchesDescriptor(actual Materialization, expected ExactDescriptor) bool {
	return actual.Namespace == expected.Namespace && actual.Name == expected.Name &&
		actual.SecretRef == expected.SecretRef && actual.Key == expected.Key && actual.Revision == expected.Revision &&
		actual.UID == expected.UID && actual.ResourceVersion == expected.ResourceVersion &&
		subtle.ConstantTimeCompare([]byte(actual.ContentSHA256), []byte(expected.ContentSHA256)) == 1
}

// validDigest accepts exactly 64 lowercase hexadecimal characters decoding to SHA-256 bytes; it checks
// representation, not the authenticity or content of a digest.
// validDigest допускает ровно 64 lowercase шестнадцатеричных символа, декодируемых в байты SHA-256;
// проверяется представление, не подлинность или содержимое digest.
func validDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
