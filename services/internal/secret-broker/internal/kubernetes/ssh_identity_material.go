package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// CreateSSHIdentity submits one immutable purpose object for an owner-prepared parsed key; transport ambiguity stays UNKNOWN, while a completed damaged success returns a terminal private rejection receipt, without implicit GET or another create.
// CreateSSHIdentity отправляет один immutable purpose object для owner-prepared parsed key; transport ambiguity остаётся UNKNOWN, а completed damaged success возвращает terminal private rejection receipt без implicit GET либо нового create.
func (store *Store) CreateSSHIdentity(ctx context.Context, provision value.SSHIdentityProvision, material []byte) (value.SSHIdentityLocator, error) {
	if ctx == nil || ctx.Err() != nil || !provision.Valid() || provision.Namespace != store.namespace || len(material) == 0 || len(material) > 16384 {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityRejected
	}
	client, err := store.sshIdentityRESTClient()
	if err != nil {
		return value.SSHIdentityLocator{}, err
	}
	owned := bytes.Clone(material)
	defer clear(owned)
	digest := sha256.Sum256(owned)
	if hex.EncodeToString(digest[:]) != provision.ContentSHA256 {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityRejected
	}
	immutable := true
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: store.namespace, Name: value.SSHIdentitySecretName(provision.IdentityRevisionRef, provision.Revision), Labels: map[string]string{sshIdentityPurposeLabel: value.SSHIdentityPurpose}, Annotations: map[string]string{
		sshIdentityRevisionAnnotation:   provision.IdentityRevisionRef,
		sshIdentityTargetAnnotation:     provision.TargetRef,
		sshIdentityGenerationAnnotation: strconv.FormatInt(provision.Revision, 10),
		sshIdentityContentAnnotation:    provision.ContentSHA256,
		sshIdentityPublicAnnotation:     provision.AuthenticationSHA256,
	}}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{sshIdentityMaterialKey: owned}}
	wire, err := json.Marshal(secret)
	if err != nil {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityRejected
	}
	response, err := store.sshIdentityHTTP(ctx, client, http.MethodPost, "", wire)
	defer clear(response.body)
	if err != nil || response.status < 200 || response.status >= 300 {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityUnknown
	}
	return sshIdentityAcknowledgement(provision, response.body, response.status == http.StatusCreated && response.json)
}

// sshIdentityAcknowledgement distinguishes complete invalid success from network ambiguity and pins an unambiguous acknowledged UID before validating its remaining material tuple; private bytes are represented only by a bounded digest.
// sshIdentityAcknowledgement отличает complete invalid success от network ambiguity и фиксирует unambiguous acknowledged UID до проверки остального material tuple; private bytes представлены только bounded digest.
func sshIdentityAcknowledgement(provision value.SSHIdentityProvision, body []byte, envelopeValid bool) (value.SSHIdentityLocator, error) {
	rejection := sshIdentityResponseRejection(body)
	if rejection.Truncated {
		return value.SSHIdentityLocator{}, rejection
	}
	created := &corev1.Secret{}
	defer func() { clear(created.Data[sshIdentityMaterialKey]) }()
	decoded, _, err := serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict).UniversalDeserializer().Decode(body, nil, created)
	if err != nil || decoded != created || created.APIVersion != "v1" || created.Kind != "Secret" {
		return value.SSHIdentityLocator{}, rejection
	}
	locator, err := sshIdentityLocator(provision, created)
	if err != nil || !envelopeValid {
		return value.SSHIdentityLocator{}, rejection
	}
	return locator, nil
}

// sshIdentityResponseRejection captures a bounded response digest and only an unambiguous exact metadata.uid before semantic decoding; unknown fields may invalidate the object without erasing a known UID witness.
// sshIdentityResponseRejection фиксирует bounded response digest и лишь unambiguous exact metadata.uid до semantic decoding; unknown fields могут отклонить object, не стирая known UID witness.
func sshIdentityResponseRejection(body []byte) *sshrepository.IdentityAcknowledgementRejection {
	digest := sha256.Sum256(body)
	result := &sshrepository.IdentityAcknowledgementRejection{ResponseSHA256: hex.EncodeToString(digest[:]), Truncated: len(body) > 65536}
	if result.Truncated {
		return result
	}
	var root map[string]json.RawMessage
	if internalrpcauth.DecodeStrictJSON(body, &root) != nil || root == nil {
		return result
	}
	for key := range root {
		if strings.EqualFold(key, "metadata") && key != "metadata" {
			return result
		}
	}
	var metadata map[string]json.RawMessage
	if internalrpcauth.DecodeStrictJSON(root["metadata"], &metadata) != nil || metadata == nil {
		return result
	}
	for key := range metadata {
		if strings.EqualFold(key, "uid") && key != "uid" {
			return result
		}
	}
	var uid string
	if json.Unmarshal(metadata["uid"], &uid) == nil && len(uid) > 0 && len(uid) <= 256 && utf8.ValidString(uid) {
		result.ObservedUID = uid
	}
	return result
}

// ObserveSSHIdentity performs only the separately admitted initial reconciliation GET; the setup owner must keep an unknown-create intent after absence and prohibit post-binding replacement adoption.
// ObserveSSHIdentity выполняет только отдельно разрешённый initial reconciliation GET; setup owner обязан сохранить unknown-create intent после absence и запретить post-binding replacement adoption.
func (store *Store) ObserveSSHIdentity(ctx context.Context, provision value.SSHIdentityProvision) (value.SSHIdentityLocator, error) {
	if ctx == nil || ctx.Err() != nil || !provision.Valid() || provision.Namespace != store.namespace {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityRejected
	}
	client, err := store.sshIdentityRESTClient()
	if err != nil {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityUnavailable
	}
	response, err := store.sshIdentityHTTP(ctx, client, http.MethodGet, value.SSHIdentitySecretName(provision.IdentityRevisionRef, provision.Revision), nil)
	defer clear(response.body)
	if err != nil || ctx.Err() != nil {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityUnavailable
	}
	if response.status == http.StatusNotFound {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityAbsent
	}
	if response.status < 200 || response.status >= 300 {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityUnavailable
	}
	return sshIdentityAcknowledgement(provision, response.body, response.status == http.StatusOK && response.json)
}

// DeleteSSHIdentity checks the previously bound immutable object, sends one UID/resourceVersion-preconditioned delete and requires authoritative absence before acknowledging cleanup; no fresh UID is adopted.
// DeleteSSHIdentity проверяет ранее bound immutable object, отправляет один delete с UID/resourceVersion preconditions и требует authoritative absence до cleanup acknowledgement; fresh UID не принимается.
func (store *Store) DeleteSSHIdentity(ctx context.Context, locator value.SSHIdentityLocator) error {
	if ctx == nil || ctx.Err() != nil || !locator.Valid() || locator.Namespace != store.namespace {
		return sshrepository.ErrIdentityRejected
	}
	secret, err := store.sshIdentityGet(ctx, locator.SecretName)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return sshrepository.ErrIdentityUnavailable
	}
	defer clear(secret.Data[sshIdentityMaterialKey])
	current, err := sshIdentityLocator(locator.Provision(), secret)
	if err != nil || current.SecretUID != locator.SecretUID || current.SecretResourceVersion != locator.SecretResourceVersion || ctx.Err() != nil {
		return sshrepository.ErrIdentityRejected
	}
	uid, version := types.UID(locator.SecretUID), locator.SecretResourceVersion
	client, err := store.sshIdentityRESTClient()
	if err != nil {
		return err
	}
	options := metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}
	wire, err := json.Marshal(options)
	if err != nil {
		return sshrepository.ErrIdentityRejected
	}
	response, err := store.sshIdentityHTTP(ctx, client, http.MethodDelete, locator.SecretName, wire)
	clear(response.body)
	if err != nil || response.status < 200 || response.status >= 300 {
		return sshrepository.ErrIdentityUnknown
	}
	remaining, readErr := store.sshIdentityGet(ctx, locator.SecretName)
	if remaining != nil {
		clear(remaining.Data[sshIdentityMaterialKey])
	}
	err = readErr
	if !apierrors.IsNotFound(err) {
		return sshrepository.ErrIdentityUnknown
	}
	return nil
}

// sshIdentityRESTClient reconstructs a purpose-only CoreV1 JSON client over the existing trusted TLS/auth transport, preserving its rate limiter without copying atomic SDK state or mutating the shared client.
// sshIdentityRESTClient создаёт purpose-only CoreV1 JSON client поверх существующего trusted TLS/auth transport, сохраняя rate limiter без копирования atomic SDK state либо изменения shared client.
func (store *Store) sshIdentityRESTClient() (*rest.RESTClient, error) {
	client, ok := store.client.CoreV1().RESTClient().(*rest.RESTClient)
	if !ok || client == nil || client.Client == nil || client.Client.Transport == nil || client.APIVersion() != corev1.SchemeGroupVersion {
		return nil, sshrepository.ErrIdentityRejected
	}
	endpoint := client.Get().URL()
	if endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "/api/v1" || endpoint.RawPath != "" || endpoint.Fragment != "" || endpoint.Opaque != "" || endpoint.ForceQuery {
		return nil, sshrepository.ErrIdentityRejected
	}
	if endpoint.RawQuery != "" {
		if endpoint.RawQuery != (url.Values{"timeout": []string{client.Client.Timeout.String()}}).Encode() {
			return nil, sshrepository.ErrIdentityRejected
		}
		endpoint.RawQuery = ""
	}
	httpClient := *client.Client
	httpClient.Jar = nil
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if httpClient.Timeout <= 0 || httpClient.Timeout > 5*time.Second {
		httpClient.Timeout = 5 * time.Second
	}
	content := rest.ClientContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json", GroupVersion: corev1.SchemeGroupVersion, Negotiator: runtime.NewClientNegotiator(scheme.Codecs.WithoutConversion(), corev1.SchemeGroupVersion)}
	isolated, err := rest.NewRESTClient(endpoint, "", content, client.GetRateLimiter(), &httpClient)
	if err != nil {
		return nil, sshrepository.ErrIdentityRejected
	}
	return isolated, nil
}

// sshIdentityGet uses the same redirect-denying private transport for reads; a TLS-authenticated redirect never selects a different namespace, endpoint or credential recipient.
// sshIdentityGet использует тот же redirect-denying private transport для чтения; TLS-authenticated redirect не выбирает другой namespace, endpoint либо credential recipient.
func (store *Store) sshIdentityGet(ctx context.Context, name string) (*corev1.Secret, error) {
	client, err := store.sshIdentityRESTClient()
	if err != nil {
		return nil, err
	}
	response, err := store.sshIdentityHTTP(ctx, client, http.MethodGet, name, nil)
	defer clear(response.body)
	if err != nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	if response.status == http.StatusNotFound {
		return nil, apierrors.NewNotFound(corev1.Resource("secrets"), name)
	}
	if response.status >= 200 && response.status < 300 && (response.status != http.StatusOK || !response.json || len(response.body) > 65536) {
		return nil, sshIdentityResponseRejection(response.body)
	}
	if response.status != http.StatusOK {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	secret := &corev1.Secret{}
	decoded, _, err := serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict).UniversalDeserializer().Decode(response.body, nil, secret)
	if err != nil || decoded != secret || secret.APIVersion != "v1" || secret.Kind != "Secret" {
		clear(secret.Data[sshIdentityMaterialKey])
		return nil, sshIdentityResponseRejection(response.body)
	}
	return secret, nil
}

// sshIdentityLocator captures object pins only after the immutable tuple and actual bytes agree; it grants no authority to commit an owner binding or accept a replacement revision.
// sshIdentityLocator фиксирует object pins только после совпадения immutable tuple и actual bytes; authority на owner binding commit либо принятие replacement revision он не выдаёт.
func sshIdentityLocator(provision value.SSHIdentityProvision, secret *corev1.Secret) (value.SSHIdentityLocator, error) {
	if _, err := sshIdentityMaterial(provision, secret); err != nil {
		return value.SSHIdentityLocator{}, err
	}
	locator := value.SSHIdentityLocator{Purpose: provision.Purpose, IdentityRevisionRef: provision.IdentityRevisionRef, TargetRef: provision.TargetRef, Revision: provision.Revision, Namespace: provision.Namespace, SecretName: secret.Name, SecretUID: string(secret.UID), SecretResourceVersion: secret.ResourceVersion, ContentSHA256: provision.ContentSHA256, AuthenticationSHA256: provision.AuthenticationSHA256}
	if !locator.Valid() {
		return value.SSHIdentityLocator{}, sshrepository.ErrIdentityRejected
	}
	return locator, nil
}

// sshIdentityMaterial validates one SSH-only object without interpreting its UID or resourceVersion, returning borrowed bytes only to the private adapter that owns the API response.
// sshIdentityMaterial проверяет SSH-only object без интерпретации UID либо resourceVersion, возвращая borrowed bytes только private adapter, владеющему API response.
func sshIdentityMaterial(provision value.SSHIdentityProvision, secret *corev1.Secret) ([]byte, error) {
	if !provision.Valid() || secret == nil || secret.Namespace != provision.Namespace || secret.Name != value.SSHIdentitySecretName(provision.IdentityRevisionRef, provision.Revision) || secret.DeletionTimestamp != nil || len(secret.Finalizers) != 0 || len(secret.OwnerReferences) != 0 ||
		secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || len(secret.StringData) != 0 || len(secret.Data) != 1 ||
		secret.Labels[sshIdentityPurposeLabel] != value.SSHIdentityPurpose || secret.Annotations[sshIdentityRevisionAnnotation] != provision.IdentityRevisionRef || secret.Annotations[sshIdentityTargetAnnotation] != provision.TargetRef || secret.Annotations[sshIdentityGenerationAnnotation] != strconv.FormatInt(provision.Revision, 10) || secret.Annotations[sshIdentityContentAnnotation] != provision.ContentSHA256 || secret.Annotations[sshIdentityPublicAnnotation] != provision.AuthenticationSHA256 {
		return nil, sshrepository.ErrIdentityRejected
	}
	material := secret.Data[sshIdentityMaterialKey]
	if len(material) == 0 || len(material) > 16384 {
		return nil, sshrepository.ErrIdentityRejected
	}
	digest := sha256.Sum256(material)
	if hex.EncodeToString(digest[:]) != provision.ContentSHA256 {
		return nil, sshrepository.ErrIdentityRejected
	}
	return material, nil
}
