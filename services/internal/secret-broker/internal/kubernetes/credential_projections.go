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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

const (
	credentialProjectionLabel      = "credential-projections.kodex.dev/runtime"
	credentialProjectionAnnotation = "credential-projections.kodex.dev/manifest"
	credentialProjectionDigest     = "credential-projections.kodex.dev/content-sha256"
	providerProjectionKey          = "provider-auth.json"
	maximumCredentialProjection    = 1 << 20
)

var (
	ErrCredentialProjectionInvalid  = errors.New("credential projection is invalid")
	ErrCredentialProjectionConflict = errors.New("credential projection conflicts with exact binding")
	credentialProjectionKeyPattern  = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,126}$`)
)

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

type ProjectionAuthority struct {
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

type ProviderProjectionBinding struct {
	AccountRef            string `json:"account_ref"`
	CredentialRevisionRef string `json:"credential_revision_ref"`
	SecretName            string `json:"secret_name"`
	SecretUID             string `json:"secret_uid"`
	SecretResourceVersion string `json:"secret_resource_version"`
	ContentSHA256         string `json:"content_sha256"`
	CredentialRevision    int64  `json:"credential_revision"`
}

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

type CredentialProjection struct {
	Namespace, SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
	Manifest                                                               CredentialProjectionManifest
}

func (store *Store) MaterializeRuntimeCredentialProjection(ctx context.Context, manifest CredentialProjectionManifest) (CredentialProjection, error) {
	if err := validateCredentialProjectionManifest(manifest, store.namespace); err != nil {
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

func validateCredentialProjectionManifest(value CredentialProjectionManifest, namespace string) error {
	assistant := value.Authority.CallerFullMethod == "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
	if namespace != "kodex-runtime" || value.Authority.ActorID == "" || value.Authority.TenantID == "" ||
		(assistant && (value.Authority.ProjectID != "" || len(value.RuntimeSecrets) != 0)) || (!assistant && value.Authority.ProjectID == "") ||
		value.Authority.ProofJTI == "" || value.Authority.SourceRevision == 0 || value.Authority.CallerCredentialRevision == 0 ||
		!validProjectionDigest(value.Authority.SourceDigestSHA256) || value.Authority.CallerWorkloadID != "runtime-controller" ||
		(!assistant && value.Authority.CallerFullMethod != "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials") ||
		value.WorkloadInstance == "" || value.LeaseRef == "" || value.Generation < 1 || value.Attempt < 1 ||
		value.RuntimeRevisionRef == "" || value.SessionRef == "" || value.TurnRef == "" || !validProjectionDigest(value.RuntimeRevisionDigest) ||
		!validProjectionDigest(value.InputDigest) || value.ExpiresAt.IsZero() || value.ProviderCredential.AccountRef == "" ||
		!value.ExpiresAt.After(time.Now()) || value.ExpiresAt.After(value.Authority.ExpiresAt) || value.ProviderCredential.CredentialRevisionRef == "" ||
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

func credentialProjectionName(manifest CredentialProjectionManifest, contentDigest string) string {
	value := strings.Join([]string{manifest.Authority.ProofJTI, manifest.LeaseRef, strconv.FormatInt(manifest.Generation, 10),
		manifest.RuntimeRevisionRef, contentDigest}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return "runtime-credentials-" + hex.EncodeToString(digest[:20])
}

func validProjectionDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == hex.EncodeToString(decoded)
}

func sameCredentialProjection(left, right CredentialProjection) bool {
	return left.Namespace == right.Namespace && left.SecretName == right.SecretName && left.SecretUID == right.SecretUID &&
		left.SecretResourceVersion == right.SecretResourceVersion && left.ContentSHA256 == right.ContentSHA256
}

func clearProjectionData(data map[string][]byte) {
	for key := range data {
		clear(data[key])
		delete(data, key)
	}
}
