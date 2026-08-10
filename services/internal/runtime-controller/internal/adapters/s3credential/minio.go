// Package s3credential реализует закрытый direct-production MinIO identity provider.
package s3credential

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	port "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/s3credential"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/security/s3policy"
	"github.com/minio/madmin-go/v3"
	miniocredentials "github.com/minio/minio-go/v7/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	credentialTTL = 15 * time.Minute
	probeTTL      = time.Minute
	stateSchema   = "mattercodex.runtime-s3-minio-identity-state/v1"
	recordSchema  = "mattercodex.runtime-s3-minio-identity-record/v1"
)

type Config struct {
	Action                    port.Action
	Namespace, Endpoint       string
	TLSServerName, CAFile     string
	AccessKeyIDFile           string
	SecretAccessKeyFile       string
	SigningKeyFile            string
	ParentUser, ParentProfile string
	Bucket, Region            string
	KMSKeyARN, KMSKeyID       string
	RequestTimeout            time.Duration
}

type addRequest struct {
	AccessKey, SecretKey, Name, Description string
	Policy                                  []byte
	Expiration                              time.Time
}

type credential struct {
	AccessKey, SecretKey string
	Expiration           time.Time
}

type accountInfo struct {
	ParentUser, Status, Policy, Name, Description string
	Expiration                                    time.Time
}

type admin interface {
	AddServiceAccount(context.Context, addRequest) (credential, error)
	InfoServiceAccount(context.Context, string) (accountInfo, error)
	DeleteServiceAccount(context.Context, string) error
	IsNotFound(error) bool
	Close()
}

type stateStore interface {
	Load(context.Context, port.Action) (state, string, error)
	Save(context.Context, port.Action, string, state) error
}

type Provider struct {
	config     Config
	admin      admin
	state      stateStore
	signingKey []byte
	now        func() time.Time
}

type state struct {
	Schema     string            `json:"schema"`
	Generation uint64            `json:"generation"`
	Records    map[string]record `json:"records"`
}

type record struct {
	Schema, ID, AccessKeyID, ExecutionID, WorkloadID, SourceExecutionID        string
	Action, ParentProfile, InputSHA256, PolicySHA256, Status                   string
	Attempt                                                                    uint32
	GrantGeneration, ExecutionVersion, Fence, CredentialGeneration, Generation uint64
	IssuedAt, ExpiresAt                                                        time.Time
	Signature                                                                  string
}

func New(config Config, secrets corev1client.SecretInterface) (*Provider, error) {
	expectedParentUser := "runtime-s3-" + string(config.Action) + "-management"
	if !config.Action.Valid() || config.Namespace == "" || secrets == nil || config.ParentUser != expectedParentUser ||
		config.ParentProfile != "runtime-s3-"+string(config.Action)+"-minio-management" ||
		config.Bucket == "" || config.Region == "" || !strings.HasPrefix(config.KMSKeyARN, "arn:") || config.KMSKeyID == "" ||
		strings.ContainsAny(config.KMSKeyID, "\x00\r\n/*?") ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 30*time.Second {
		return nil, errors.New("runtime MinIO identity provider configuration is invalid")
	}
	signingKey, err := readMaterial(config.SigningKeyFile, 32, 4096)
	if err != nil {
		return nil, errors.New("read runtime MinIO identity signing material")
	}
	client, err := newAdmin(config)
	if err != nil {
		return nil, err
	}
	return &Provider{config: config, admin: client, state: &kubernetesStateStore{secrets: secrets}, signingKey: signingKey, now: time.Now}, nil
}

func newAdmin(config Config) (admin, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "" ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil || config.TLSServerName == "" {
		return nil, errors.New("runtime MinIO admin endpoint is invalid")
	}
	caRaw, err := readMaterial(config.CAFile, 1, 1<<20)
	if err != nil {
		return nil, errors.New("read runtime MinIO admin CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse runtime MinIO admin CA")
	}
	accessKey, err := readMaterial(config.AccessKeyIDFile, 3, 128)
	if err != nil {
		return nil, errors.New("read runtime MinIO management access key")
	}
	secretKey, err := readMaterial(config.SecretAccessKeyFile, 8, 256)
	if err != nil {
		return nil, errors.New("read runtime MinIO management secret key")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSHandshakeTimeout = config.RequestTimeout
	transport.ResponseHeaderTimeout = config.RequestTimeout
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots}
	client, err := madmin.NewWithOptions(endpoint.Host, &madmin.Options{
		Creds: miniocredentials.NewStaticV4(string(accessKey), string(secretKey), ""), Secure: true, Transport: transport,
	})
	if err != nil {
		return nil, errors.New("create runtime MinIO admin client")
	}
	return &madminClient{client: client, transport: transport}, nil
}

func (provider *Provider) Issue(ctx context.Context, request port.Request) (port.Issue, error) {
	expected, inputDigest, err := provider.validateRequest(request)
	if err != nil {
		return port.Issue{}, err
	}
	for attempt := 0; attempt < 4; attempt++ {
		current, resourceVersion, loadErr := provider.state.Load(ctx, request.Action)
		if loadErr != nil || provider.validateState(current) != nil {
			return port.Issue{}, errors.New("read runtime MinIO identity state")
		}
		id := recordID(request)
		if existing, ok := current.Records[id]; ok {
			if !provider.recordMatches(existing, request, inputDigest, expected) || existing.Status == "revoked" {
				return port.Issue{}, errors.New("runtime MinIO credential idempotency conflict")
			}
			return provider.rejoinOrFinish(ctx, current, resourceVersion, existing, request, expected)
		}
		changed, revokeErr := provider.revokeOlderAttempts(ctx, &current, request)
		if revokeErr != nil {
			return port.Issue{}, revokeErr
		}
		if changed {
			if saveErr := provider.state.Save(ctx, request.Action, resourceVersion, current); apierrors.IsConflict(saveErr) {
				continue
			} else if saveErr != nil {
				return port.Issue{}, errors.New("persist revoked runtime MinIO identity state")
			}
			continue
		}
		issuedAt := provider.now().UTC().Truncate(time.Second)
		entry := record{Schema: recordSchema, ID: id, AccessKeyID: provider.accessKey(id),
			ExecutionID: request.Execution.ID, WorkloadID: request.Execution.WorkloadID,
			SourceExecutionID: request.SourceExecutionID, Action: string(request.Action), ParentProfile: provider.config.ParentProfile,
			InputSHA256: inputDigest, PolicySHA256: digest(expected), Status: "issuing", Attempt: request.Execution.Attempt,
			GrantGeneration: request.Execution.GrantGeneration, ExecutionVersion: request.Execution.Version, Fence: request.Execution.Fence,
			CredentialGeneration: current.Generation + 1, Generation: current.Generation + 1,
			IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(credentialTTL)}
		entry.Signature = provider.sign(entry)
		current.Generation = entry.Generation
		current.Records[id] = entry
		if saveErr := provider.state.Save(ctx, request.Action, resourceVersion, current); apierrors.IsConflict(saveErr) {
			continue
		} else if saveErr != nil {
			return port.Issue{}, errors.New("persist issuing runtime MinIO identity record")
		}
		return provider.finishIssue(ctx, entry, request, expected)
	}
	return port.Issue{}, errors.New("runtime MinIO identity state CAS exhausted")
}

func (provider *Provider) Ready(ctx context.Context, action port.Action) error {
	if action != provider.config.Action {
		return errors.New("runtime MinIO identity action is not registered")
	}
	current, _, err := provider.state.Load(ctx, action)
	if err != nil || provider.validateState(current) != nil {
		return errors.New("read runtime MinIO identity state")
	}
	nonce := make([]byte, 16)
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return errors.New("generate runtime MinIO readiness nonce")
	}
	probeID := digest(nonce)
	accessKey, secretKey := provider.accessKey(probeID), provider.secretKey(probeID)
	policy := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"s3:*","Resource":"*"}]}`)
	expiresAt := provider.now().UTC().Truncate(time.Second).Add(probeTTL)
	name, description := provider.name(probeID), "schema=mattercodex.runtime-s3-minio-probe/v1;digest="+digest(policy)
	created, err := provider.admin.AddServiceAccount(ctx, addRequest{AccessKey: accessKey, SecretKey: secretKey,
		Name: name, Description: description, Policy: policy, Expiration: expiresAt})
	if err != nil || created.AccessKey != accessKey || created.SecretKey != secretKey {
		return errors.New("create bounded runtime MinIO readiness credential")
	}
	info, err := provider.admin.InfoServiceAccount(ctx, accessKey)
	if err != nil || !provider.infoMatches(info, policy, name, description, expiresAt) {
		_ = provider.admin.DeleteServiceAccount(ctx, accessKey)
		return errors.New("read back bounded runtime MinIO readiness credential")
	}
	if err = provider.admin.DeleteServiceAccount(ctx, accessKey); err != nil {
		return errors.New("delete bounded runtime MinIO readiness credential")
	}
	if _, err = provider.admin.InfoServiceAccount(ctx, accessKey); err == nil || !provider.admin.IsNotFound(err) {
		return errors.New("confirm bounded runtime MinIO readiness credential revocation")
	}
	return nil
}

func (provider *Provider) Check(ctx context.Context, request port.Request) error {
	expected, inputDigest, err := provider.validateRequest(request)
	if err != nil {
		return err
	}
	current, _, err := provider.state.Load(ctx, request.Action)
	if err != nil || provider.validateState(current) != nil {
		return errors.New("read runtime MinIO identity state")
	}
	entry, ok := current.Records[recordID(request)]
	if !ok || entry.Status != "active" || !provider.recordMatches(entry, request, inputDigest, expected) ||
		!entry.ExpiresAt.After(provider.now().UTC()) {
		return errors.New("runtime MinIO credential record is unavailable")
	}
	info, err := provider.admin.InfoServiceAccount(ctx, entry.AccessKeyID)
	if err != nil || !provider.infoMatches(info, expected, provider.name(entry.ID), provider.description(entry), entry.ExpiresAt) {
		return errors.New("runtime MinIO credential readback mismatch")
	}
	return nil
}

func (provider *Provider) Revoke(ctx context.Context, request port.Request, _ port.Issue) error {
	expected, inputDigest, err := provider.validateRequest(request)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		current, resourceVersion, loadErr := provider.state.Load(ctx, request.Action)
		if loadErr != nil || provider.validateState(current) != nil {
			return errors.New("read runtime MinIO identity state")
		}
		entry, ok := current.Records[recordID(request)]
		if !ok || !provider.recordMatches(entry, request, inputDigest, expected) {
			return errors.New("runtime MinIO revoke record is unknown")
		}
		if entry.Status != "revoked" {
			if err = provider.deleteAndConfirm(ctx, entry.AccessKeyID); err != nil {
				return err
			}
			entry.Status = "revoked"
			entry.Generation = current.Generation + 1
			entry.Signature = provider.sign(entry)
			current.Generation = entry.Generation
			current.Records[entry.ID] = entry
		} else if _, infoErr := provider.admin.InfoServiceAccount(ctx, entry.AccessKeyID); infoErr == nil || !provider.admin.IsNotFound(infoErr) {
			return errors.New("runtime MinIO revoked credential is still available")
		}
		if saveErr := provider.state.Save(ctx, request.Action, resourceVersion, current); apierrors.IsConflict(saveErr) {
			continue
		} else if saveErr != nil {
			return errors.New("persist revoked runtime MinIO identity record")
		}
		return nil
	}
	return errors.New("runtime MinIO identity state CAS exhausted")
}

func (provider *Provider) Close() { provider.admin.Close() }

func (provider *Provider) validateRequest(request port.Request) ([]byte, string, error) {
	if request.Action != provider.config.Action || request.Execution.Validate() != nil || request.SourceExecutionID == "" ||
		len(request.PolicyRaw) == 0 || len(request.PolicyRaw) > 4096 {
		return nil, "", errors.New("runtime MinIO credential request is invalid")
	}
	policy, err := s3policy.Build(request.Execution, request.Action, s3policy.Config{Bucket: provider.config.Bucket,
		Region: provider.config.Region, KMSKeyARN: provider.config.KMSKeyARN, KMSKeyID: provider.config.KMSKeyID}, s3policy.DialectMinIO, provider.now())
	if err != nil || policy.SourceExecutionID != request.SourceExecutionID || !bytes.Equal(policy.Raw, request.PolicyRaw) {
		return nil, "", errors.New("runtime MinIO credential policy mismatch")
	}
	input, err := json.Marshal(struct {
		ExecutionID, OrganizationID, ProjectID, SessionID, WorkloadID, WorkloadSPIFFEID string
		SourceExecutionID, ImmutableInputSHA256, RuntimeRevisionSHA256, PolicySHA256    string
		Action                                                                          port.Action
		Attempt                                                                         uint32
		GrantGeneration, Version, Fence                                                 uint64
	}{request.Execution.ID, request.Execution.OrganizationID, request.Execution.ProjectID, request.Execution.SessionID,
		request.Execution.WorkloadID, request.Execution.WorkloadSPIFFEID, request.SourceExecutionID,
		request.Execution.ImmutableInputSHA256, request.Execution.RuntimeRevisionSHA256, digest(policy.Raw), request.Action,
		request.Execution.Attempt, request.Execution.GrantGeneration, request.Execution.Version, request.Execution.Fence})
	if err != nil {
		return nil, "", errors.New("encode runtime MinIO credential authority")
	}
	return policy.Raw, digest(input), nil
}

func (provider *Provider) rejoinOrFinish(ctx context.Context, current state, resourceVersion string, entry record, request port.Request, policy []byte) (port.Issue, error) {
	info, err := provider.admin.InfoServiceAccount(ctx, entry.AccessKeyID)
	if err == nil {
		if !provider.infoMatches(info, policy, provider.name(entry.ID), provider.description(entry), entry.ExpiresAt) {
			return port.Issue{}, errors.New("runtime MinIO credential readback mismatch")
		}
		if entry.Status == "active" {
			return provider.issueResult(entry), nil
		}
		return provider.activate(ctx, current, resourceVersion, entry)
	}
	if !provider.admin.IsNotFound(err) || entry.Status == "active" {
		return port.Issue{}, errors.New("read back runtime MinIO credential")
	}
	return provider.finishIssue(ctx, entry, request, policy)
}

func (provider *Provider) finishIssue(ctx context.Context, entry record, _ port.Request, policy []byte) (port.Issue, error) {
	secret := provider.secretKey(entry.ID)
	created, err := provider.admin.AddServiceAccount(ctx, addRequest{AccessKey: entry.AccessKeyID, SecretKey: secret,
		Name: provider.name(entry.ID), Description: provider.description(entry), Policy: policy, Expiration: entry.ExpiresAt})
	if err != nil {
		if _, infoErr := provider.admin.InfoServiceAccount(ctx, entry.AccessKeyID); infoErr != nil {
			return port.Issue{}, errors.New("create runtime MinIO execution credential")
		}
	} else if created.AccessKey != entry.AccessKeyID || created.SecretKey != secret || !created.Expiration.UTC().Equal(entry.ExpiresAt) {
		return port.Issue{}, errors.New("runtime MinIO execution credential response mismatch")
	}
	info, err := provider.admin.InfoServiceAccount(ctx, entry.AccessKeyID)
	if err != nil || !provider.infoMatches(info, policy, provider.name(entry.ID), provider.description(entry), entry.ExpiresAt) {
		return port.Issue{}, errors.New("runtime MinIO execution credential readback mismatch")
	}
	current, resourceVersion, err := provider.state.Load(ctx, port.Action(entry.Action))
	if err != nil || provider.validateState(current) != nil {
		return port.Issue{}, errors.New("read runtime MinIO identity state")
	}
	actual, ok := current.Records[entry.ID]
	if !ok || actual.Signature != entry.Signature || actual.Status != "issuing" {
		return port.Issue{}, errors.New("runtime MinIO issuing record changed")
	}
	return provider.activate(ctx, current, resourceVersion, actual)
}

func (provider *Provider) activate(ctx context.Context, current state, resourceVersion string, entry record) (port.Issue, error) {
	for attempt := 0; attempt < 4; attempt++ {
		active := entry
		active.Status = "active"
		active.Generation = current.Generation + 1
		active.Signature = provider.sign(active)
		current.Generation = active.Generation
		current.Records[active.ID] = active
		if err := provider.state.Save(ctx, port.Action(active.Action), resourceVersion, current); err == nil {
			return provider.issueResult(active), nil
		} else if !apierrors.IsConflict(err) {
			return port.Issue{}, errors.New("persist active runtime MinIO identity record")
		}
		reloaded, nextVersion, err := provider.state.Load(ctx, port.Action(entry.Action))
		if err != nil || provider.validateState(reloaded) != nil {
			return port.Issue{}, errors.New("read runtime MinIO identity state")
		}
		actual, ok := reloaded.Records[entry.ID]
		if !ok || actual.CredentialGeneration != entry.CredentialGeneration || actual.InputSHA256 != entry.InputSHA256 {
			return port.Issue{}, errors.New("runtime MinIO issuing record changed")
		}
		if actual.Status == "active" {
			return provider.issueResult(actual), nil
		}
		if actual.Status != "issuing" {
			return port.Issue{}, errors.New("runtime MinIO issuing record changed")
		}
		current, resourceVersion, entry = reloaded, nextVersion, actual
	}
	return port.Issue{}, errors.New("runtime MinIO identity state CAS exhausted")
}

func (provider *Provider) revokeOlderAttempts(ctx context.Context, current *state, request port.Request) (bool, error) {
	changed := false
	for id, entry := range current.Records {
		if entry.ExecutionID != request.Execution.ID || entry.Action != string(request.Action) || entry.Status == "revoked" {
			continue
		}
		if entry.Attempt >= request.Execution.Attempt {
			return false, errors.New("runtime MinIO credential generation rollback")
		}
		if err := provider.deleteAndConfirm(ctx, entry.AccessKeyID); err != nil {
			return false, err
		}
		entry.Status = "revoked"
		entry.Generation = current.Generation + 1
		entry.Signature = provider.sign(entry)
		current.Generation = entry.Generation
		current.Records[id] = entry
		changed = true
	}
	return changed, nil
}

func (provider *Provider) deleteAndConfirm(ctx context.Context, accessKey string) error {
	if err := provider.admin.DeleteServiceAccount(ctx, accessKey); err != nil && !provider.admin.IsNotFound(err) {
		return errors.New("delete runtime MinIO execution credential")
	}
	if _, err := provider.admin.InfoServiceAccount(ctx, accessKey); err == nil || !provider.admin.IsNotFound(err) {
		return errors.New("confirm runtime MinIO execution credential revocation")
	}
	return nil
}

func (provider *Provider) infoMatches(info accountInfo, policy []byte, name, description string, expiration time.Time) bool {
	var actualPolicy, expectedPolicy any
	return info.ParentUser == provider.config.ParentUser && info.Status == "on" && info.Name == name && info.Description == description &&
		info.Expiration.After(provider.now().UTC()) &&
		info.Expiration.UTC().Equal(expiration.UTC()) && json.Unmarshal([]byte(info.Policy), &actualPolicy) == nil &&
		json.Unmarshal(policy, &expectedPolicy) == nil && deepEqualJSON(actualPolicy, expectedPolicy)
}

func (provider *Provider) validateState(current state) error {
	if current.Schema != stateSchema || current.Records == nil {
		return errors.New("runtime MinIO identity state schema mismatch")
	}
	for id, entry := range current.Records {
		if id != entry.ID || entry.Schema != recordSchema || entry.CredentialGeneration == 0 ||
			entry.CredentialGeneration > entry.Generation || entry.Generation == 0 || entry.Generation > current.Generation ||
			(entry.Status != "issuing" && entry.Status != "active" && entry.Status != "revoked") ||
			!hmac.Equal([]byte(entry.Signature), []byte(provider.sign(entry))) {
			return errors.New("runtime MinIO identity record mismatch")
		}
	}
	return nil
}

func (provider *Provider) recordMatches(entry record, request port.Request, inputDigest string, policy []byte) bool {
	return entry.ExecutionID == request.Execution.ID && entry.WorkloadID == request.Execution.WorkloadID &&
		entry.SourceExecutionID == request.SourceExecutionID && entry.Action == string(request.Action) &&
		entry.ParentProfile == provider.config.ParentProfile && entry.InputSHA256 == inputDigest && entry.PolicySHA256 == digest(policy) &&
		entry.Attempt == request.Execution.Attempt && entry.GrantGeneration == request.Execution.GrantGeneration &&
		entry.ExecutionVersion == request.Execution.Version && entry.Fence == request.Execution.Fence && entry.ExpiresAt.Sub(entry.IssuedAt) == credentialTTL
}

func (provider *Provider) sign(entry record) string {
	entry.Signature = ""
	raw, _ := json.Marshal(entry)
	mac := hmac.New(sha256.New, provider.signingKey)
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func (provider *Provider) accessKey(id string) string {
	mac := hmac.New(sha256.New, provider.signingKey)
	_, _ = mac.Write([]byte("access-key/v1:" + id))
	return "MCX" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(mac.Sum(nil))[:17]
}

func (provider *Provider) secretKey(id string) string {
	mac := hmac.New(sha256.New, provider.signingKey)
	_, _ = mac.Write([]byte("secret-key/v1:" + id))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:40]
}

func (provider *Provider) name(id string) string {
	return "mcx-" + string(provider.config.Action) + "-" + id[:16]
}

func (provider *Provider) description(entry record) string {
	return "schema=" + recordSchema + ";record=" + entry.ID + ";generation=" + strconv.FormatUint(entry.CredentialGeneration, 10) + ";digest=" + entry.InputSHA256
}

func (provider *Provider) issueResult(entry record) port.Issue {
	return port.Issue{AccessKeyID: entry.AccessKeyID, SecretAccessKey: provider.secretKey(entry.ID), ExpiresAt: entry.ExpiresAt,
		AssumedRoleARN: "minio:service-account:" + entry.AccessKeyID, SessionName: provider.name(entry.ID)}
}

func recordID(request port.Request) string {
	raw := strings.Join([]string{request.Execution.ID, strconv.FormatUint(uint64(request.Execution.Attempt), 10), string(request.Action)}, ":")
	return digest([]byte(raw))
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func deepEqualJSON(left, right any) bool {
	leftRaw, leftErr := json.Marshal(left)
	rightRaw, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func readMaterial(path string, minimum, maximum int) ([]byte, error) {
	if path == "" {
		return nil, errors.New("material path is empty")
	}
	raw, err := os.ReadFile(path)
	raw = bytes.TrimSpace(raw)
	if err != nil || len(raw) < minimum || len(raw) > maximum {
		return nil, errors.New("material is invalid")
	}
	return raw, nil
}

type kubernetesStateStore struct{ secrets corev1client.SecretInterface }

func stateSecretName(action port.Action) string {
	return "runtime-s3-" + string(action) + "-minio-identity-records"
}

func (store *kubernetesStateStore) Load(ctx context.Context, action port.Action) (state, string, error) {
	if !action.Valid() {
		return state{}, "", errors.New("runtime MinIO identity state action is invalid")
	}
	secret, err := store.secrets.Get(ctx, stateSecretName(action), metav1.GetOptions{})
	if err != nil || secret.Type != corev1.SecretTypeOpaque || secret.Immutable != nil && *secret.Immutable || len(secret.Data) != 1 ||
		secret.Annotations["runtime.mattercodex.dev/state-kind"] != "minio-service-account-records" ||
		secret.Annotations["runtime.mattercodex.dev/action"] != string(action) {
		return state{}, "", errors.New("runtime MinIO identity state Secret mismatch")
	}
	var current state
	decoder := json.NewDecoder(bytes.NewReader(secret.Data["state.json"]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&current) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return state{}, "", errors.New("decode runtime MinIO identity state")
	}
	return current, secret.ResourceVersion, nil
}

func (store *kubernetesStateStore) Save(ctx context.Context, action port.Action, resourceVersion string, current state) error {
	if !action.Valid() || resourceVersion == "" {
		return errors.New("runtime MinIO identity state CAS is invalid")
	}
	raw, err := json.Marshal(current)
	if err != nil || len(raw) > 900<<10 {
		return errors.New("encode runtime MinIO identity state")
	}
	secret, err := store.secrets.Get(ctx, stateSecretName(action), metav1.GetOptions{})
	if err != nil || secret.ResourceVersion != resourceVersion || secret.Name != stateSecretName(action) {
		return apierrors.NewConflict(corev1.Resource("secrets"), stateSecretName(action), errors.New("resourceVersion changed"))
	}
	updated := secret.DeepCopy()
	updated.Data = map[string][]byte{"state.json": raw}
	_, err = store.secrets.Update(ctx, updated, metav1.UpdateOptions{})
	return err
}

type madminClient struct {
	client    *madmin.AdminClient
	transport *http.Transport
}

func (client *madminClient) AddServiceAccount(ctx context.Context, request addRequest) (credential, error) {
	expiresAt := request.Expiration
	created, err := client.client.AddServiceAccount(ctx, madmin.AddServiceAccountReq{Policy: json.RawMessage(request.Policy),
		AccessKey: request.AccessKey, SecretKey: request.SecretKey, Name: request.Name, Description: request.Description, Expiration: &expiresAt})
	return credential{AccessKey: created.AccessKey, SecretKey: created.SecretKey, Expiration: created.Expiration}, err
}

func (client *madminClient) InfoServiceAccount(ctx context.Context, accessKey string) (accountInfo, error) {
	info, err := client.client.InfoServiceAccount(ctx, accessKey)
	result := accountInfo{ParentUser: info.ParentUser, Status: info.AccountStatus, Policy: info.Policy, Name: info.Name, Description: info.Description}
	if info.Expiration != nil {
		result.Expiration = info.Expiration.UTC()
	}
	return result, err
}

func (client *madminClient) DeleteServiceAccount(ctx context.Context, accessKey string) error {
	return client.client.DeleteServiceAccount(ctx, accessKey)
}

func (*madminClient) IsNotFound(err error) bool {
	var response madmin.ErrorResponse
	return errors.As(err, &response) && (response.Code == "XMinioInvalidIAMCredentials" || response.Code == "NoSuchServiceAccount")
}

func (client *madminClient) Close() { client.transport.CloseIdleConnections() }
