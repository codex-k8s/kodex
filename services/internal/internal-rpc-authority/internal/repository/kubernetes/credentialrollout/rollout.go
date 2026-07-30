package credentialrollout

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"
)

const maxResponseBytes = 1 << 20

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Config задаёт exact Kubernetes API boundary controlled rollout.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	Deployments   []string
	Timeout       time.Duration
}

// Rollout атомарно привязывает pod template consumers к immutable intent.
type Rollout struct {
	config Config
	client *http.Client
}

// New создаёт клиент Kubernetes API с exact server name и CA.
func New(config Config) (*Rollout, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "mattercodex-system" ||
		len(config.Deployments) != 2 ||
		config.Deployments[0] != "internal-rpc-authority-publisher" ||
		config.Deployments[1] != "internal-rpc-authority-readback-attestor" ||
		config.Timeout < time.Second ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("database credential rollout configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("kubernetes API CA is invalid")
	}
	return &Rollout{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
					RootCAs:    rootCAs,
					ServerName: config.TLSServerName,
				},
				ForceAttemptHTTP2: true,
			},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
	}, nil
}

// Close освобождает простаивающие connections.
func (rollout *Rollout) Close() {
	rollout.client.CloseIdleConnections()
}

// RolloutCurrent изменяет только pod-template annotations двух consumers.
func (rollout *Rollout) RolloutCurrent(
	ctx context.Context,
	requestID string,
	canonicalDigest string,
) error {
	return rollout.apply(ctx, "CURRENT_PROMOTED", requestID, canonicalDigest)
}

// RolloutNext перезапускает consumers для CSI readback staged NEXT до promotion.
func (rollout *Rollout) RolloutNext(
	ctx context.Context,
	requestID string,
	canonicalDigest string,
) error {
	return rollout.apply(ctx, "NEXT_STAGED", requestID, canonicalDigest)
}

func (rollout *Rollout) apply(
	ctx context.Context,
	phase string,
	requestID string,
	canonicalDigest string,
) error {
	if !uuidPattern.MatchString(requestID) || !digestPattern.MatchString(canonicalDigest) {
		return errors.New("database credential rollout intent is invalid")
	}
	tokenRaw, err := os.ReadFile(rollout.config.TokenFile)
	if err != nil || len(tokenRaw) < 32 || len(tokenRaw) > 16384 {
		return errors.New("read Kubernetes API service account token")
	}
	for _, deployment := range rollout.config.Deployments {
		endpoint := rollout.config.Address +
			"/apis/apps/v1/namespaces/" + url.PathEscape(rollout.config.Namespace) +
			"/deployments/" + url.PathEscape(deployment)
		body, err := json.Marshal(map[string]any{
			"metadata": map[string]any{
				"annotations": map[string]string{
					"mattercodex.dev/credential-rollout-request-id": requestID,
				},
			},
			"spec": map[string]any{
				"template": map[string]any{
					"metadata": map[string]any{
						"annotations": map[string]string{
							"mattercodex.dev/credential-rollout-request-id":    requestID,
							"mattercodex.dev/credential-rollout-digest-sha256": canonicalDigest,
							"mattercodex.dev/credential-rollout-phase":         phase,
						},
					},
				},
			},
		})
		if err != nil {
			return errors.New("encode database credential rollout patch")
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPatch,
			endpoint,
			bytes.NewReader(body),
		)
		if err != nil {
			return errors.New("construct database credential rollout request")
		}
		request.Header.Set("Authorization", "Bearer "+string(bytes.TrimSpace(tokenRaw)))
		request.Header.Set("Content-Type", "application/merge-patch+json")
		response, err := rollout.client.Do(request)
		if err != nil {
			return errors.New("perform database credential rollout request")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return errors.New("database credential rollout request rejected")
		}
	}
	return nil
}
