// Package readinessgrant выпускает короткоживущие application grants для
// startup/readiness и узких фоновых workload-операций direct-production.
package readinessgrant

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

const (
	grantType             = "mattercodex-application-grant+jws"
	credentialPurpose     = "WORKLOAD_READINESS_GRANT"
	maximumResponseBytes  = 16 << 10
	readinessActorID      = "63dfc7d7-9439-5e8d-8953-24f975da8f32"
	readinessOrganization = "d9b072a0-3980-57c0-a6fe-289b7a608f31"
)

// Target связывает один signer с точным workload и одним ключом Secret.
type Target struct {
	ProducerID    string
	WorkloadID    string
	CallerSPIFFE  string
	Issuer        string
	Audience      string
	PrivateJWK    string
	SecretName    string
	SecretDataKey string
}

// DefaultTargets возвращает закрытый набор startup grants direct-production.
func DefaultTargets(signerDirectory string) []Target {
	return []Target{
		readinessTarget(signerDirectory, "control-api-gateway", "control-plane.control-api-readiness", "control-api-gateway-application-grant", "readiness.jwt"),
		readinessTarget(signerDirectory, "automation-scheduler", "control-plane.automation-readiness", "automation-scheduler-application-grant", "application-grant.jws"),
		automationTarget(signerDirectory),
		readinessTarget(signerDirectory, "integration-gateway", "control-plane.integration-readiness", "integration-gateway-application-grant", "readiness.jwt"),
		readinessTarget(signerDirectory, "interaction-gateway", "control-plane.owner-gate-readiness", "interaction-gateway-application-grant", "readiness.jwt"),
		readinessTarget(signerDirectory, "runtime-controller", "control-plane.runtime-readiness", "runtime-controller-application-grant", "application-grant.jws"),
	}
}

func automationTarget(directory string) Target {
	return Target{
		ProducerID: "control-plane.automation", WorkloadID: "automation-scheduler",
		CallerSPIFFE: "spiffe://mattercodex.local/ns/mattercodex-system/sa/automation-scheduler",
		Issuer:       "https://control-plane.mattercodex-system.svc.cluster.local/authority/automation-scheduler",
		Audience:     "urn:mattercodex:automation-occurrence",
		PrivateJWK:   filepath.Join(directory, "automation-scheduler-operation.private.jwk"),
		SecretName:   "automation-scheduler-application-grant", SecretDataKey: "operation-grant.jws",
	}
}

func readinessTarget(directory, workload, producer, secret, key string) Target {
	return Target{
		ProducerID: producer, WorkloadID: workload,
		CallerSPIFFE: "spiffe://mattercodex.local/ns/mattercodex-system/sa/" + workload,
		Issuer:       "https://control-plane.mattercodex-system.svc.cluster.local/authority/readiness/" + workload,
		Audience:     "urn:mattercodex:workload-readiness:" + workload,
		PrivateJWK:   filepath.Join(directory, workload+".private.jwk"),
		SecretName:   secret, SecretDataKey: key,
	}
}

// SecretPatcher меняет только заранее разрешённый ключ существующего Secret.
type SecretPatcher interface {
	PatchSecret(context.Context, string, string, string, []byte) error
}

type signer struct {
	target Target
	key    internalrpcauth.ES256Key
}

// Rotator атомарно выпускает полный набор readiness grants через Kubernetes API.
type Rotator struct {
	namespace string
	ttl       time.Duration
	interval  time.Duration
	patcher   SecretPatcher
	signers   []signer
	now       func() time.Time
	ready     atomic.Bool
}

// New создаёт закрытый ротатор. Он не принимает произвольные claims из env.
func New(namespace string, ttl, interval time.Duration, targets []Target, patcher SecretPatcher) (*Rotator, error) {
	if namespace == "" || strings.TrimSpace(namespace) != namespace ||
		ttl < 2*time.Minute || ttl > 5*time.Minute ||
		interval < 15*time.Second || interval >= ttl/2 || patcher == nil || len(targets) == 0 {
		return nil, errors.New("readiness grant rotator configuration is invalid")
	}
	rotator := &Rotator{namespace: namespace, ttl: ttl, interval: interval, patcher: patcher, now: time.Now}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		identity := target.SecretName + "\x00" + target.SecretDataKey
		if target.ProducerID == "" || target.WorkloadID == "" || target.CallerSPIFFE == "" ||
			target.Issuer == "" || target.Audience == "" || !filepath.IsAbs(target.PrivateJWK) ||
			target.SecretName == "" || target.SecretDataKey == "" {
			return nil, errors.New("readiness grant target is invalid")
		}
		if _, duplicate := seen[identity]; duplicate {
			return nil, errors.New("readiness grant target is duplicated")
		}
		seen[identity] = struct{}{}
		raw, err := readPrivateJWK(target.PrivateJWK)
		if err != nil {
			return nil, err
		}
		key, err := internalrpcauth.ParsePrivateJWK(raw)
		if err != nil {
			return nil, errors.New("parse readiness grant signer")
		}
		rotator.signers = append(rotator.signers, signer{target: target, key: key})
	}
	return rotator, nil
}

type claims struct {
	Version        int    `json:"v"`
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	JTI            string `json:"jti"`
	Revision       uint64 `json:"revision"`
	TenantOwner    bool   `json:"tenant_owner"`
	WorkloadID     string `json:"workload_id"`
	CallerSPIFFEID string `json:"caller_spiffe_id"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

// Rotate выпускает набор с одним timestamp, но независимыми JTI и подписями.
func (rotator *Rotator) Rotate(ctx context.Context) error {
	now := rotator.now().UTC().Truncate(time.Second)
	revision := uint64(now.Unix())
	if revision == 0 {
		return errors.New("readiness grant revision is invalid")
	}
	for _, current := range rotator.signers {
		payload := claims{
			Version: 1, Issuer: current.target.Issuer, Audience: current.target.Audience,
			Subject: readinessActorID, OrganizationID: readinessOrganization,
			JTI: uuid.NewString(), Revision: revision, WorkloadID: current.target.WorkloadID,
			CallerSPIFFEID: current.target.CallerSPIFFE, IssuedAt: now.Unix(),
			NotBefore: now.Unix(), ExpiresAt: now.Add(rotator.ttl).Unix(),
		}
		compact, err := internalrpcauth.SignCanonicalJSON(payload, current.key,
			internalrpcauth.ProtectedHeaderExpectation{Type: grantType, KeyID: current.key.KeyID})
		if err != nil {
			rotator.ready.Store(false)
			return fmt.Errorf("sign readiness grant for %s: %w", current.target.WorkloadID, err)
		}
		if err := rotator.patcher.PatchSecret(ctx, rotator.namespace, current.target.SecretName,
			current.target.SecretDataKey, []byte(compact)); err != nil {
			rotator.ready.Store(false)
			return fmt.Errorf("patch readiness grant for %s: %w", current.target.WorkloadID, err)
		}
	}
	rotator.ready.Store(true)
	return nil
}

// Run обновляет grants сразу при старте и затем до истечения текущего набора.
func (rotator *Rotator) Run(ctx context.Context) error {
	if err := rotator.Rotate(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(rotator.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := rotator.Rotate(ctx); err != nil {
				return err
			}
		}
	}
}

// Ready сообщает, был ли полностью записан последний набор.
func (rotator *Rotator) Ready() bool { return rotator != nil && rotator.ready.Load() }

func readPrivateJWK(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("readiness grant signer file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read readiness grant signer")
	}
	return raw, nil
}

// KubernetesPatcher обновляет Secret через TLS и короткоживущий SA token.
type KubernetesPatcher struct {
	address   string
	tokenFile string
	client    *http.Client
}

// NewKubernetesPatcher создаёт точный клиент Kubernetes API.
func NewKubernetesPatcher(address, serverName, caFile, tokenFile string, timeout time.Duration) (*KubernetesPatcher, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" ||
		serverName == "" || !filepath.IsAbs(caFile) || !filepath.IsAbs(tokenFile) ||
		timeout < time.Second || timeout > 30*time.Second {
		return nil, errors.New("Kubernetes patch client configuration is invalid")
	}
	caRaw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse Kubernetes API CA")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots}
	return &KubernetesPatcher{address: strings.TrimSuffix(address, "/"), tokenFile: tokenFile,
		client: &http.Client{Transport: transport, Timeout: timeout}}, nil
}

// PatchSecret применяет merge patch только к одному data key.
func (patcher *KubernetesPatcher) PatchSecret(ctx context.Context, namespace, name, key string, value []byte) error {
	if patcher == nil || namespace == "" || name == "" || key == "" || len(value) == 0 {
		return errors.New("Kubernetes Secret patch input is invalid")
	}
	token, err := os.ReadFile(patcher.tokenFile)
	if err != nil || len(token) == 0 || len(token) > 32<<10 || strings.TrimSpace(string(token)) != string(token) {
		return errors.New("Kubernetes API token is invalid")
	}
	body, err := json.Marshal(map[string]any{"data": map[string]string{key: base64.StdEncoding.EncodeToString(value)}})
	if err != nil {
		return errors.New("encode Kubernetes Secret patch")
	}
	endpoint := fmt.Sprintf("%s/api/v1/namespaces/%s/secrets/%s", patcher.address,
		url.PathEscape(namespace), url.PathEscape(name))
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return errors.New("create Kubernetes Secret patch")
	}
	request.Header.Set("Authorization", "Bearer "+string(token))
	request.Header.Set("Content-Type", "application/merge-patch+json")
	response, err := patcher.client.Do(request)
	if err != nil {
		return errors.New("execute Kubernetes Secret patch")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumResponseBytes))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Kubernetes Secret patch returned status %d", response.StatusCode)
	}
	return nil
}
