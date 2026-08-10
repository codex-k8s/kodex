// Package credential реализует узкий Vault KV v2 lifecycle Mattermost bot token.
package credential

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	domaincredential "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/credential"
	"github.com/google/uuid"
)

const maximumVaultResponseBytes = 64 << 10

type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	AuthMount     string
	Role          string
	Mount         string
	PathPrefix    string
	Timeout       time.Duration
}

type Store struct {
	config      Config
	base        *url.URL
	client      *http.Client
	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

type writeRequest struct {
	Options struct {
		CAS uint64 `json:"cas"`
	} `json:"options"`
	Data secretData `json:"data"`
}

type secretData struct {
	BindingID     string `json:"binding_id"`
	Status        string `json:"status"`
	Token         string `json:"token,omitempty"`
	ContentSHA256 string `json:"content_sha256"`
}

type writeResponse struct {
	Data struct {
		Version uint64 `json:"version"`
	} `json:"data"`
}

type readResponse struct {
	Data struct {
		Data     secretData `json:"data"`
		Metadata struct {
			Version uint64 `json:"version"`
		} `json:"metadata"`
	} `json:"data"`
}

type loginResponse struct {
	Auth struct {
		ClientToken   string `json:"client_token"`
		LeaseDuration int64  `json:"lease_duration"`
	} `json:"auth"`
}

func New(config Config) (*Store, error) {
	base, err := url.Parse(config.Address)
	if err != nil || base.Scheme != "https" || base.Hostname() != config.TLSServerName || base.Path != "" ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		config.Mount == "" || strings.Contains(config.Mount, "/") ||
		config.AuthMount == "" || strings.Contains(config.AuthMount, "/") ||
		config.Role == "" || strings.ContainsAny(config.Role, " /\r\n\x00") ||
		config.PathPrefix == "" || strings.HasPrefix(config.PathPrefix, "/") || strings.Contains(config.PathPrefix, "..") ||
		config.Timeout < time.Second || config.Timeout > 30*time.Second {
		return nil, errors.New("Vault bot credential configuration is invalid")
	}
	for _, file := range []string{config.CAFile, config.TokenFile} {
		if !filepath.IsAbs(file) {
			return nil, errors.New("Vault bot credential runtime path is invalid")
		}
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read Vault CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse Vault CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
		RootCAs: roots,
	}, ForceAttemptHTTP2: true, MaxIdleConns: 8, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second,
		ResponseHeaderTimeout: config.Timeout}
	return &Store{config: config, base: base,
		client: &http.Client{Transport: transport, Timeout: config.Timeout}}, nil
}

func (store *Store) MaterializeBotToken(ctx context.Context, bindingID, token string) (domaincredential.Materialized, error) {
	if uuid.Validate(bindingID) != nil || token == "" || len(token) > 4096 {
		return domaincredential.Materialized{}, errors.New("Vault bot credential materialization input is invalid")
	}
	digest := sha256.Sum256([]byte(token))
	contentSHA := hex.EncodeToString(digest[:])
	request := writeRequest{Data: secretData{BindingID: bindingID, Status: "ACTIVE", Token: token, ContentSHA256: contentSHA}}
	request.Options.CAS = 0
	var response writeResponse
	status, err := store.doJSON(ctx, http.MethodPost, store.dataPath(bindingID), request, &response)
	if err != nil {
		if status == http.StatusBadRequest || status == http.StatusConflict {
			existing, readErr := store.RecoverBotToken(ctx, bindingID)
			if readErr == nil && existing.ContentSHA256 == contentSHA {
				return existing, nil
			}
		}
		return domaincredential.Materialized{}, errors.New("materialize Vault bot credential")
	}
	if response.Data.Version == 0 {
		return domaincredential.Materialized{}, errors.New("Vault bot credential version is unavailable")
	}
	return domaincredential.Materialized{BindingID: bindingID, SecretRef: store.secretRef(bindingID),
		Version: response.Data.Version, ContentSHA256: contentSHA}, nil
}

func (store *Store) RecoverBotToken(ctx context.Context, bindingID string) (domaincredential.Materialized, error) {
	data, err := store.read(ctx, bindingID, 0)
	if err != nil || data.Data.Data.Status != "ACTIVE" || data.Data.Data.BindingID != bindingID ||
		data.Data.Data.Token == "" || data.Data.Metadata.Version == 0 {
		return domaincredential.Materialized{}, errors.New("recover Vault bot credential")
	}
	digest := sha256.Sum256([]byte(data.Data.Data.Token))
	contentSHA := hex.EncodeToString(digest[:])
	if contentSHA != data.Data.Data.ContentSHA256 {
		return domaincredential.Materialized{}, errors.New("Vault bot credential digest mismatch")
	}
	return domaincredential.Materialized{BindingID: bindingID, SecretRef: store.secretRef(bindingID),
		Version: data.Data.Metadata.Version, ContentSHA256: contentSHA}, nil
}

func (store *Store) ReadBotToken(ctx context.Context, bindingID string, version uint64, expectedSHA string) (string, error) {
	if uuid.Validate(bindingID) != nil || version == 0 || len(expectedSHA) != 64 {
		return "", errors.New("Vault bot credential read input is invalid")
	}
	data, err := store.read(ctx, bindingID, version)
	if err != nil || data.Data.Metadata.Version != version || data.Data.Data.BindingID != bindingID ||
		data.Data.Data.Status != "ACTIVE" || data.Data.Data.Token == "" || data.Data.Data.ContentSHA256 != expectedSHA {
		return "", errors.New("Vault bot credential readback mismatch")
	}
	digest := sha256.Sum256([]byte(data.Data.Data.Token))
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return "", errors.New("Vault bot credential content mismatch")
	}
	return data.Data.Data.Token, nil
}

func (store *Store) RevokeBotToken(ctx context.Context, bindingID string, version uint64) (bool, error) {
	if uuid.Validate(bindingID) != nil || version == 0 {
		return false, errors.New("Vault bot credential revoke input is invalid")
	}
	if store.CheckBotTokenRevoked(ctx, bindingID, version) == nil {
		return false, nil
	}
	request := writeRequest{Data: secretData{BindingID: bindingID, Status: "REVOKED",
		ContentSHA256: strings.Repeat("0", 64)}}
	request.Options.CAS = version
	var response writeResponse
	if _, err := store.doJSON(ctx, http.MethodPost, store.dataPath(bindingID), request, &response); err != nil ||
		response.Data.Version <= version {
		if store.CheckBotTokenRevoked(ctx, bindingID, version) == nil {
			return true, nil
		}
		return false, errors.New("revoke Vault bot credential")
	}
	if err := store.CheckBotTokenRevoked(ctx, bindingID, version); err != nil {
		return false, err
	}
	return true, nil
}

func (store *Store) CheckBotTokenRevoked(ctx context.Context, bindingID string, activeVersion uint64) error {
	if uuid.Validate(bindingID) != nil || activeVersion == 0 {
		return errors.New("Vault bot credential revoke readback input is invalid")
	}
	current, err := store.read(ctx, bindingID, 0)
	if err != nil || current.Data.Data.BindingID != bindingID || current.Data.Data.Status != "REVOKED" ||
		current.Data.Data.Token != "" || current.Data.Data.ContentSHA256 != strings.Repeat("0", 64) ||
		current.Data.Metadata.Version <= activeVersion {
		return errors.New("Vault bot credential revoke readback mismatch")
	}
	return nil
}

func (store *Store) Check(ctx context.Context) error {
	// Read отсутствующего случайного binding проверяет тот же exact KV v2 prefix
	// и workload auth path, не создавая credential и не расширяя policy.
	status, _ := store.doJSON(ctx, http.MethodGet, store.dataPath(uuid.NewString()), nil, nil)
	if status != http.StatusNotFound {
		return errors.New("Vault bot credential working path is not ready")
	}
	return nil
}

func (store *Store) Close() { store.client.CloseIdleConnections() }

func (store *Store) read(ctx context.Context, bindingID string, version uint64) (readResponse, error) {
	query := ""
	if version > 0 {
		query = "?version=" + fmt.Sprint(version)
	}
	var response readResponse
	_, err := store.doJSON(ctx, http.MethodGet, store.dataPath(bindingID)+query, nil, &response)
	return response, err
}

func (store *Store) dataPath(bindingID string) string {
	return "/v1/" + url.PathEscape(store.config.Mount) + "/data/" +
		strings.Trim(path.Clean(store.config.PathPrefix+"/"+bindingID), "/")
}

func (store *Store) secretRef(bindingID string) string {
	return "vault://" + store.config.Mount + "/data/" +
		strings.Trim(path.Clean(store.config.PathPrefix+"/"+bindingID), "/")
}

func (store *Store) doJSON(ctx context.Context, method, requestPath string, input, output any) (int, error) {
	var encoded []byte
	if input != nil {
		var err error
		encoded, err = json.Marshal(input)
		if err != nil {
			return 0, errors.New("encode Vault request")
		}
	}
	token, err := store.workloadToken(ctx)
	if err != nil {
		return 0, err
	}
	status, err := store.requestJSON(ctx, method, requestPath, encoded, output, token)
	if status != http.StatusForbidden {
		return status, err
	}
	store.mu.Lock()
	store.token, store.tokenExpiry = "", time.Time{}
	store.mu.Unlock()
	token, err = store.workloadToken(ctx)
	if err != nil {
		return status, err
	}
	return store.requestJSON(ctx, method, requestPath, encoded, output, token)
}

func (store *Store) workloadToken(ctx context.Context) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.token != "" && time.Now().UTC().Add(30*time.Second).Before(store.tokenExpiry) {
		return store.token, nil
	}
	jwtRaw, err := os.ReadFile(store.config.TokenFile)
	jwt := strings.TrimSpace(string(jwtRaw))
	if err != nil || jwt == "" || len(jwt) > 16<<10 {
		return "", errors.New("read Vault workload identity")
	}
	input, err := json.Marshal(map[string]string{"role": store.config.Role, "jwt": jwt})
	jwt = ""
	if err != nil {
		return "", errors.New("encode Vault workload login")
	}
	var response loginResponse
	status, err := store.requestJSON(ctx, http.MethodPost,
		"/v1/auth/"+url.PathEscape(store.config.AuthMount)+"/login", input, &response, "")
	if err != nil || status != http.StatusOK || response.Auth.ClientToken == "" ||
		response.Auth.LeaseDuration < 60 || response.Auth.LeaseDuration > 24*60*60 {
		return "", errors.New("authenticate Vault workload identity")
	}
	store.token = response.Auth.ClientToken
	store.tokenExpiry = time.Now().UTC().Add(time.Duration(response.Auth.LeaseDuration) * time.Second * 4 / 5)
	return store.token, nil
}

func (store *Store) requestJSON(ctx context.Context, method, requestPath string, encoded []byte,
	output any, token string,
) (int, error) {
	var body io.Reader
	if len(encoded) > 0 {
		body = bytes.NewReader(encoded)
	}
	target := *store.base
	parts := strings.SplitN(requestPath, "?", 2)
	target.Path = parts[0]
	if len(parts) == 2 {
		target.RawQuery = parts[1]
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return 0, errors.New("build Vault request")
	}
	if token != "" {
		request.Header.Set("X-Vault-Token", token)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := store.client.Do(request)
	if err != nil {
		return 0, errors.New("execute Vault request")
	}
	defer response.Body.Close()
	bounded, err := io.ReadAll(io.LimitReader(response.Body, maximumVaultResponseBytes+1))
	if err != nil || len(bounded) > maximumVaultResponseBytes {
		return response.StatusCode, errors.New("read bounded Vault response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, errors.New("Vault request rejected")
	}
	if output != nil && (len(bounded) == 0 || json.Unmarshal(bounded, output) != nil) {
		return response.StatusCode, errors.New("decode Vault response")
	}
	return response.StatusCode, nil
}
