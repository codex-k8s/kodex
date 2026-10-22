package kubernetes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// credentialProjectionLabel selects runtime credential projections for recovery; exact object and
	// manifest checks remain required after selection.
	// credentialProjectionLabel выбирает runtime credential projections для recovery; после отбора по метке
	// по-прежнему обязательны проверки точного объекта и manifest.
	credentialProjectionLabel = "credential-projections.kodex.dev/runtime"
	// credentialProjectionAnnotation stores the serialized execution and source binding manifest alongside
	// projected bytes without embedding the source values in metadata.
	// credentialProjectionAnnotation хранит сериализованный manifest привязок выполнения и источников рядом
	// с проецируемыми байтами без самих значений источников в metadata.
	credentialProjectionAnnotation = "credential-projections.kodex.dev/manifest"
	// credentialProjectionDigest records the framed digest of the complete projection Data map for later
	// exact integrity readback.
	// credentialProjectionDigest записывает digest всей Data map с разграничением элементов для последующей
	// точной проверки целостности.
	credentialProjectionDigest = "credential-projections.kodex.dev/content-sha256"
	// providerProjectionKey reserves the provider-auth filename in the projection; runtime secret names
	// cannot collide with it.
	// providerProjectionKey резервирует имя файла provider-auth в проекции; имена runtime-секретов не могут
	// с ним пересекаться.
	providerProjectionKey = "provider-auth.json"
	// maximumCredentialProjection declares a one-MiB projection size value but is not currently referenced
	// by the materialization or parsing paths; it must not be mistaken for an enforced limit.
	// maximumCredentialProjection объявляет значение размера проекции один МиБ, но сейчас не используется
	// путями materialization или разбора; его нельзя считать применяемым ограничением.
	maximumCredentialProjection = 1 << 20
)

var (
	// ErrCredentialProjectionInvalid rejects invalid binding manifests and new expired projections before
	// creating an object; it also covers manifest serialization failure or metadata above 64 KiB.
	// ErrCredentialProjectionInvalid отклоняет неверные manifest привязок и новые просроченные проекции до
	// создания объекта; также охватывает ошибку сериализации manifest или metadata свыше 64 КиБ.
	ErrCredentialProjectionInvalid = errors.New("credential projection is invalid")
	// ErrCredentialProjectionConflict reports malformed existing projections, mismatched exact bindings or
	// stale delete preconditions without returning projected values.
	// ErrCredentialProjectionConflict сообщает о повреждённой существующей проекции, несовпадении точных
	// привязок или устаревших предусловиях удаления без возврата значений проекции.
	ErrCredentialProjectionConflict = errors.New("credential projection conflicts with exact binding")
	// credentialProjectionKeyPattern admits uppercase environment-style destination names of one to 127
	// ASCII characters; uniqueness and reserved-key checks are separate.
	// credentialProjectionKeyPattern допускает destination-имена в стиле env из одного–127 ASCII-символов
	// верхнего регистра; уникальность и резервный ключ проверяются отдельно.
	credentialProjectionKeyPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)
)

// CredentialProjectionManifest pins an execution lease, runtime revision, attempt, expiry and exact
// provider/runtime secret sources. It describes already established authority; merely supplying this
// structure does not grant access.
// CredentialProjectionManifest закрепляет lease выполнения, runtime revision, attempt, срок и точные
// источники provider/runtime секретов. Он описывает уже установленные полномочия; передача структуры
// сама по себе доступа не даёт.
type CredentialProjectionManifest struct {
	Authority             ProjectionAuthority              `json:"authority"`
	WorkloadInstance      string                           `json:"workload_instance"`
	LeaseRef              string                           `json:"lease_ref"`
	Generation            int64                            `json:"generation"`
	RuntimeRevisionRef    string                           `json:"runtime_revision_ref"`
	RuntimeRevisionDigest string                           `json:"runtime_revision_digest"`
	SessionRef            string                           `json:"session_ref"`
	TurnRef               string                           `json:"turn_ref"`
	Attempt               int32                            `json:"attempt"`
	InputDigest           string                           `json:"input_digest"`
	ProviderCredential    ProviderProjectionBinding        `json:"provider_credential"`
	RuntimeSecrets        []RuntimeSecretProjectionBinding `json:"runtime_secrets"`
	ExpiresAt             time.Time                        `json:"expires_at"`
}

// ProjectionAuthority carries verified caller provenance or the trusted-cluster execution snapshot.
// Storage checks profile shape and manifest consistency, while proof verification and authoritative
// owner lookup belong above this adapter.
// ProjectionAuthority несёт проверенное происхождение вызывающего или execution snapshot
// trusted-cluster. Хранилище проверяет форму профиля и согласованность manifest, а проверка proof и
// авторитетный поиск владельца выполняются выше адаптера.
type ProjectionAuthority struct {
	RPCProfile               string    `json:"rpc_profile,omitempty"`
	ActorID                  string    `json:"actor_id"`
	TenantID                 string    `json:"tenant_id"`
	ProjectID                string    `json:"project_id"`
	SourceDigestSHA256       string    `json:"source_digest_sha256"`
	ProofJTI                 string    `json:"proof_jti"`
	CallerWorkloadID         string    `json:"caller_workload_id"`
	CallerFullMethod         string    `json:"caller_full_method"`
	SourceRevision           uint64    `json:"source_revision"`
	CallerCredentialRevision uint64    `json:"caller_credential_revision"`
	ExpiresAt                time.Time `json:"expires_at"`
}

// ProviderProjectionBinding pins the account and credential revision plus exact Kubernetes Secret name,
// UID, resourceVersion and bytes digest selected by the owner.
// ProviderProjectionBinding закрепляет выбранные владельцем account и credential revision вместе с
// точными именем Secret Kubernetes, UID, resourceVersion и digest байтов.
type ProviderProjectionBinding struct {
	AccountRef            string `json:"account_ref"`
	CredentialRevisionRef string `json:"credential_revision_ref"`
	SecretName            string `json:"secret_name"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
	CredentialRevision    int64  `json:"credential_revision"`
}

// RuntimeSecretProjectionBinding maps one admitted destination name to an exact namespace, source key,
// secret revision and Kubernetes object identity; it contains no plaintext.
// RuntimeSecretProjectionBinding связывает одно допущенное destination-имя с точными namespace, source
// key, revision секрета и identity объекта Kubernetes; plaintext отсутствует.
type RuntimeSecretProjectionBinding struct {
	Name                  string `json:"name"`
	SecretRef             string `json:"secret_ref"`
	Revision              int64  `json:"revision"`
	Namespace             string `json:"namespace"`
	SecretName            string `json:"secret_name"`
	SecretKey             string `json:"secret_key"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
}

// CredentialProjection returns the materialized projection's exact API pins, aggregate data digest and
// decoded manifest, excluding every projected secret value.
// CredentialProjection возвращает точные API-привязки материализованной проекции, общий digest Data и
// разобранный manifest без значений проецируемых секретов.
type CredentialProjection struct {
	Namespace, SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
	Manifest                                                               CredentialProjectionManifest
}

// MaterializeRuntimeCredentialProjection validates a live manifest, reads only exact source descriptors
// and creates one immutable projection with a deterministic name. Existing objects require an identical
// manifest and verified data digest; temporary value buffers are cleared, not returned.
// MaterializeRuntimeCredentialProjection проверяет действующий manifest, читает только точные source
// descriptors и создаёт immutable проекцию с детерминированным именем. Для существующего объекта нужны
// идентичный manifest и проверенный digest Data; временные буферы значений обнуляются, а не
// возвращаются.
func (store *Store) MaterializeRuntimeCredentialProjection(ctx context.Context, manifest CredentialProjectionManifest) (CredentialProjection, error) {
	if err := validateActiveCredentialProjectionManifest(manifest, store.namespace); err != nil {
		return CredentialProjection{}, err
	}
	providerDescriptor := ProviderCredentialDescriptor{SecretName: manifest.ProviderCredential.SecretName,
		SecretUID: manifest.ProviderCredential.SecretUID, SecretResourceVersion: manifest.ProviderCredential.SecretResourceVersion,
		ContentSHA256: manifest.ProviderCredential.ContentSHA256}
	provider, err := store.ReadProviderCredentialExact(ctx, manifest.ProviderCredential.AccountRef, providerDescriptor)
	if err != nil {
		return CredentialProjection{}, err
	}
	defer clear(provider)
	data := map[string][]byte{providerProjectionKey: provider}
	for _, source := range manifest.RuntimeSecrets {
		descriptor := ExactDescriptor{Namespace: source.Namespace, Name: source.SecretName, SecretRef: source.SecretRef,
			Key: source.SecretKey, Revision: source.Revision, UID: source.SecretUID,
			ResourceVersion: source.SecretResourceVersion, ContentSHA256: source.ContentSHA256}
		_, value, err := store.ReadExactValue(ctx, descriptor)
		if err != nil {
			clearProjectionData(data)
			return CredentialProjection{}, err
		}
		data[source.Name] = value
	}
	defer clearProjectionData(data)
	contentDigest := projectionDataDigest(data)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil || len(manifestJSON) > 64<<10 {
		return CredentialProjection{}, ErrCredentialProjectionInvalid
	}
	name := credentialProjectionName(manifest, contentDigest)
	immutable := true
	wanted := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: store.namespace,
		Labels:      map[string]string{credentialProjectionLabel: "true", providerManagedByLabel: providerSecretBrokerManager, providerPartOfLabel: "kodex"},
		Annotations: map[string]string{credentialProjectionAnnotation: string(manifestJSON), credentialProjectionDigest: contentDigest}},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data}
	created, err := store.client.CoreV1().Secrets(store.namespace).Create(ctx, wanted, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = store.client.CoreV1().Secrets(store.namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return CredentialProjection{}, errors.New("create runtime credential projection")
	}
	defer clearSecretData(created)
	projection, err := credentialProjectionFromSecret(created, true)
	if err != nil || !reflect.DeepEqual(projection.Manifest, manifest) {
		return CredentialProjection{}, ErrCredentialProjectionConflict
	}
	return projection, nil
}

// ListRuntimeCredentialProjections verifies each labeled object's manifest and data digest, clears
// processed value buffers and sorts metadata by Secret name. Expired manifests remain readable for
// cleanup; a malformed item aborts the list.
// ListRuntimeCredentialProjections проверяет manifest и digest Data каждого объекта с меткой, обнуляет
// обработанные буферы значений и сортирует metadata по имени Secret. Просроченные manifest доступны для
// cleanup; повреждённый элемент прерывает список.
func (store *Store) ListRuntimeCredentialProjections(ctx context.Context) ([]CredentialProjection, error) {
	selector := labels.Set{credentialProjectionLabel: "true"}.AsSelector().String()
	list, err := store.client.CoreV1().Secrets(store.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, errors.New("list runtime credential projections")
	}
	result := make([]CredentialProjection, 0, len(list.Items))
	for index := range list.Items {
		item, parseErr := credentialProjectionFromSecret(&list.Items[index], true)
		clearSecretData(&list.Items[index])
		if parseErr != nil {
			return nil, parseErr
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SecretName < result[j].SecretName })
	return result, nil
}

// DeleteRuntimeCredentialProjection rereads and validates the projection, compares exact API pins and
// aggregate digest, deletes with UID/resourceVersion preconditions and requires absence readback. It
// never deletes source secrets; absence is idempotent success.
// DeleteRuntimeCredentialProjection перечитывает и проверяет проекцию, сравнивает точные API-привязки и
// общий digest, удаляет с предусловиями UID/resourceVersion и требует чтения отсутствия. Исходные
// секреты не удаляются; отсутствие идемпотентно.
func (store *Store) DeleteRuntimeCredentialProjection(ctx context.Context, expected CredentialProjection) error {
	secret, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, expected.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read runtime credential projection for deletion")
	}
	defer clearSecretData(secret)
	actual, err := credentialProjectionFromSecret(secret, true)
	if err != nil || !sameCredentialProjection(actual, expected) {
		return ErrCredentialProjectionConflict
	}
	uid := types.UID(expected.SecretUID)
	resourceVersion := expected.SecretResourceVersion
	err = store.client.CoreV1().Secrets(store.namespace).Delete(ctx, expected.SecretName, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}})
	if apierrors.IsConflict(err) || apierrors.IsInvalid(err) {
		return ErrCredentialProjectionConflict
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete runtime credential projection")
	}
	readback, err := store.client.CoreV1().Secrets(store.namespace).Get(ctx, expected.SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read back runtime credential projection deletion")
	}
	clearSecretData(readback)
	return ErrCredentialProjectionConflict
}

// credentialProjectionFromSecret checks immutable managed metadata, decodes and validates the manifest
// and optionally verifies the data digest. Standard json.Unmarshal is not a duplicate-key or
// unknown-field guard; expiry freshness is intentionally left to the create path.
// credentialProjectionFromSecret проверяет immutable управляемые metadata, разбирает и проверяет
// manifest и при необходимости digest Data. Обычный json.Unmarshal не запрещает duplicate keys или
// неизвестные поля; проверка актуальности срока намеренно оставлена пути создания.
func credentialProjectionFromSecret(secret *corev1.Secret, checkData bool) (CredentialProjection, error) {
	if secret == nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque ||
		secret.Labels[credentialProjectionLabel] != "true" || secret.Labels[providerManagedByLabel] != providerSecretBrokerManager ||
		secret.UID == "" || secret.ResourceVersion == "" {
		return CredentialProjection{}, ErrCredentialProjectionConflict
	}
	var manifest CredentialProjectionManifest
	if json.Unmarshal([]byte(secret.Annotations[credentialProjectionAnnotation]), &manifest) != nil ||
		validateCredentialProjectionManifest(manifest, secret.Namespace) != nil {
		return CredentialProjection{}, ErrCredentialProjectionConflict
	}
	digest := secret.Annotations[credentialProjectionDigest]
	if !validProjectionDigest(digest) || checkData && subtle.ConstantTimeCompare([]byte(projectionDataDigest(secret.Data)), []byte(digest)) != 1 {
		return CredentialProjection{}, ErrCredentialProjectionConflict
	}
	return CredentialProjection{Namespace: secret.Namespace, SecretName: secret.Name, SecretUID: string(secret.UID),
		SecretResourceVersion: secret.ResourceVersion, ContentSHA256: digest, Manifest: manifest}, nil
}

// validateCredentialProjectionManifest checks explicit signed/trusted profile shape, allowed caller
// method and method-specific project scope, source pins, at most 64 unique runtime destinations and
// expiry bounded by authority. It does not verify a signature, query owners or reject already elapsed
// expiry.
// validateCredentialProjectionManifest проверяет форму явного signed/trusted профиля, допустимый caller
// method и его project scope, source-привязки, не более 64 уникальных runtime destination и срок не
// позже authority. Подпись, поиск владельцев и уже прошедший срок здесь не проверяются.
func validateCredentialProjectionManifest(value CredentialProjectionManifest, namespace string) error {
	if value.Authority.RPCProfile == transportprofile.TrustedCluster {
		if value.Authority.ProofJTI != "" || value.Generation < 1 || value.Authority.SourceRevision != uint64(value.Generation) ||
			value.Authority.SourceDigestSHA256 != value.RuntimeRevisionDigest {
			return ErrCredentialProjectionInvalid
		}
	} else if value.Authority.RPCProfile != "" || value.Authority.ProofJTI == "" {
		return ErrCredentialProjectionInvalid
	}
	assistant := value.Authority.CallerFullMethod == "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
	if namespace != "kodex-runtime" || value.Authority.ActorID == "" || value.Authority.TenantID == "" ||
		(assistant && value.Authority.ProjectID != "") || (!assistant && value.Authority.ProjectID == "") ||
		value.Authority.SourceRevision == 0 || value.Authority.CallerCredentialRevision == 0 ||
		!validProjectionDigest(value.Authority.SourceDigestSHA256) || value.Authority.CallerWorkloadID != "runtime-controller" ||
		(!assistant && value.Authority.CallerFullMethod != "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials") ||
		value.WorkloadInstance == "" || value.LeaseRef == "" || value.Generation < 1 || value.Attempt < 1 ||
		value.RuntimeRevisionRef == "" || value.SessionRef == "" || value.TurnRef == "" || !validProjectionDigest(value.RuntimeRevisionDigest) ||
		!validProjectionDigest(value.InputDigest) || value.ExpiresAt.IsZero() || value.ProviderCredential.AccountRef == "" ||
		value.ExpiresAt.After(value.Authority.ExpiresAt) || value.ProviderCredential.CredentialRevisionRef == "" ||
		value.ProviderCredential.SecretName == "" || value.ProviderCredential.SecretUID == "" || value.ProviderCredential.SecretResourceVersion == "" ||
		value.ProviderCredential.CredentialRevision < 1 || !validProjectionDigest(value.ProviderCredential.ContentSHA256) ||
		len(value.RuntimeSecrets) > 64 {
		return ErrCredentialProjectionInvalid
	}
	seen := map[string]struct{}{providerProjectionKey: {}}
	for _, item := range value.RuntimeSecrets {
		if !credentialProjectionKeyPattern.MatchString(item.Name) || item.SecretRef == "" || item.Revision < 1 || item.Namespace != namespace ||
			item.SecretName == "" || item.SecretKey == "" || item.SecretUID == "" || item.SecretResourceVersion == "" ||
			!validProjectionDigest(item.ContentSHA256) || strings.ContainsAny(item.Name, "/\\") {
			return ErrCredentialProjectionInvalid
		}
		if _, duplicate := seen[item.Name]; duplicate {
			return ErrCredentialProjectionInvalid
		}
		seen[item.Name] = struct{}{}
	}
	return nil
}

// validateActiveCredentialProjectionManifest adds current expiry freshness to structural manifest
// validation for new materializations; cleanup parsing deliberately uses the non-active validator.
// validateActiveCredentialProjectionManifest добавляет актуальность срока к структурной проверке
// manifest для новых materialization; разбор при cleanup намеренно использует validator без этой
// проверки.
func validateActiveCredentialProjectionManifest(value CredentialProjectionManifest, namespace string) error {
	if err := validateCredentialProjectionManifest(value, namespace); err != nil || !value.ExpiresAt.After(time.Now()) {
		return ErrCredentialProjectionInvalid
	}
	return nil
}

// projectionDataDigest sorts destination keys and hashes decimal-length-framed key/value bytes, making
// map iteration order irrelevant and preserving entry boundaries; this digest is not an authorization
// proof.
// projectionDataDigest сортирует destination-ключи и хеширует байты key/value с десятичными длинами,
// устраняя зависимость от порядка map и сохраняя границы элементов; digest не является доказательством
// полномочий.
func projectionDataDigest(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	for _, key := range keys {
		_, _ = digest.Write([]byte(strconv.Itoa(len(key)) + ":" + key + ":" + strconv.Itoa(len(data[key])) + ":"))
		_, _ = digest.Write(data[key])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// credentialProjectionName hashes proof JTI, lease, generation, runtime revision reference and content
// digest separated by NUL, using 20 digest bytes in the name. Other manifest fields are checked on
// readback, not encoded as authority by the name.
// credentialProjectionName хеширует разделённые NUL proof JTI, lease, generation, ссылку runtime
// revision и digest содержимого, используя 20 байтов digest в имени. Остальные поля manifest проверяются
// при чтении, а имя не кодирует полномочия.
func credentialProjectionName(manifest CredentialProjectionManifest, contentDigest string) string {
	value := strings.Join([]string{manifest.Authority.ProofJTI, manifest.LeaseRef, strconv.FormatInt(manifest.Generation, 10),
		manifest.RuntimeRevisionRef, contentDigest}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "runtime-credentials-" + hex.EncodeToString(digest[:20])
}

// validProjectionDigest accepts a SHA-256-length lowercase hexadecimal representation by decoding and
// re-encoding it; acceptance does not authenticate the stated content.
// validProjectionDigest допускает lowercase шестнадцатеричное представление длины SHA-256 через
// декодирование и повторное кодирование; успешная проверка не подтверждает подлинность содержимого.
func validProjectionDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

// sameCredentialProjection compares namespace, object name, UID, resourceVersion and aggregate digest
// only; manifest parsing and semantic validation must occur separately.
// sameCredentialProjection сравнивает только namespace, имя объекта, UID, resourceVersion и общий
// digest; разбор и смысловая проверка manifest выполняются отдельно.
func sameCredentialProjection(left, right CredentialProjection) bool {
	return left.Namespace == right.Namespace && left.SecretName == right.SecretName && left.SecretUID == right.SecretUID &&
		left.SecretResourceVersion == right.SecretResourceVersion && left.ContentSHA256 == right.ContentSHA256
}

// clearProjectionData zeroes each projected value slice and removes its map entry; independent copies of
// those bytes remain the responsibility of their owners.
// clearProjectionData обнуляет каждый срез проецируемого значения и удаляет элемент map; независимые
// копии байтов обязаны очищать их владельцы.
func clearProjectionData(data map[string][]byte) {
	for key := range data {
		clear(data[key])
		delete(data, key)
	}
}
