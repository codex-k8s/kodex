// Package secret реализует exact TLS Vault KV v2 boundary без сохранения raw
// provider credential в PostgreSQL, audit или diagnostics.
package secret

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
)

const maximumVaultResponseBytes = 256 << 10

type Config struct {
	Address, TLSServerName, CAFile, Role, AuthMount, KVMount, PathPrefix, ServiceAccountTokenFile string
	Timeout                                                                                       time.Duration
	ReadOnly                                                                                      bool
}

type Vault struct {
	config         Config
	client         *http.Client
	mu             sync.Mutex
	token          string
	tokenExpiresAt time.Time
}

func NewVault(config Config) (*Vault, error) {
	parsed, err := url.Parse(config.Address)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != config.TLSServerName || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!filepath.IsAbs(config.CAFile) || !filepath.IsAbs(config.ServiceAccountTokenFile) || config.Role == "" || config.AuthMount == "" || config.KVMount == "" || config.PathPrefix == "" ||
		config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("Vault secret boundary configuration is invalid")
	}
	ca, err := os.ReadFile(config.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read Vault CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse Vault CA")
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots}, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: config.Timeout}
	return &Vault{config: config, client: &http.Client{Transport: transport, Timeout: config.Timeout}}, nil
}

func (vault *Vault) login(ctx context.Context) (string, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	now := time.Now().UTC()
	if vault.token != "" && vault.tokenExpiresAt.After(now.Add(30*time.Second)) {
		return vault.token, nil
	}
	raw, err := os.ReadFile(vault.config.ServiceAccountTokenFile)
	jwt := strings.TrimSpace(string(raw))
	if err != nil || jwt == "" || len(jwt) > 32<<10 {
		return "", errors.New("read Vault Kubernetes identity")
	}
	body, _ := json.Marshal(map[string]string{"role": vault.config.Role, "jwt": jwt})
	var response struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration uint64 `json:"lease_duration"`
		} `json:"auth"`
	}
	if err = vault.request(ctx, http.MethodPost, "v1/auth/"+path.Clean(vault.config.AuthMount)+"/login", "", body, &response); err != nil {
		return "", err
	}
	if response.Auth.ClientToken == "" || len(response.Auth.ClientToken) > 16<<10 || response.Auth.LeaseDuration < 60 {
		return "", errors.New("Vault login response is invalid")
	}
	vault.token, response.Auth.ClientToken = response.Auth.ClientToken, ""
	vault.tokenExpiresAt = now.Add(time.Duration(response.Auth.LeaseDuration) * time.Second)
	return vault.token, nil
}

func (vault *Vault) Put(ctx context.Context, ref string, secret []byte) (secretstore.Version, error) {
	if !vault.validRef(ref) || len(secret) == 0 || len(secret) > 128<<10 {
		return secretstore.Version{}, errors.New("Vault secret input is invalid")
	}
	token, err := vault.login(ctx)
	if err != nil {
		return secretstore.Version{}, err
	}
	sum := sha256.Sum256(secret)
	contentDigest := hex.EncodeToString(sum[:])
	if stored, version, readErr := vault.Get(ctx, ref); readErr == nil {
		storedSum := sha256.Sum256(stored)
		for index := range stored {
			stored[index] = 0
		}
		if hex.EncodeToString(storedSum[:]) != contentDigest {
			return secretstore.Version{}, errors.New("Vault immutable credential conflict")
		}
		return version, nil
	}
	body, _ := json.Marshal(map[string]any{"data": map[string]string{"value": base64.StdEncoding.EncodeToString(secret), "content_digest": contentDigest}, "options": map[string]uint64{"cas": 0}})
	var response struct {
		Data struct {
			Version uint64 `json:"version"`
		} `json:"data"`
	}
	if err = vault.request(ctx, http.MethodPost, vault.dataPath(ref), token, body, &response); err != nil {
		return secretstore.Version{}, err
	}
	if response.Data.Version == 0 {
		return secretstore.Version{}, errors.New("Vault write response is invalid")
	}
	stored, version, err := vault.Get(ctx, ref)
	if err != nil {
		return secretstore.Version{}, err
	}
	storedSum := sha256.Sum256(stored)
	for index := range stored {
		stored[index] = 0
	}
	if version.Version != response.Data.Version || hex.EncodeToString(storedSum[:]) != contentDigest {
		return secretstore.Version{}, errors.New("Vault credential readback mismatch")
	}
	return version, nil
}

func (vault *Vault) Get(ctx context.Context, ref string) ([]byte, secretstore.Version, error) {
	if !vault.validRef(ref) {
		return nil, secretstore.Version{}, errors.New("Vault secret reference is invalid")
	}
	token, err := vault.login(ctx)
	if err != nil {
		return nil, secretstore.Version{}, err
	}
	var response struct {
		Data struct {
			Data struct {
				Value         string `json:"value"`
				ContentDigest string `json:"content_digest"`
			} `json:"data"`
			Metadata struct {
				Version      uint64 `json:"version"`
				DeletionTime string `json:"deletion_time"`
				Destroyed    bool   `json:"destroyed"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err = vault.request(ctx, http.MethodGet, vault.dataPath(ref), token, nil, &response); err != nil {
		return nil, secretstore.Version{}, err
	}
	raw, err := base64.StdEncoding.DecodeString(response.Data.Data.Value)
	if err != nil || len(raw) == 0 || response.Data.Metadata.Version == 0 || response.Data.Metadata.Destroyed || response.Data.Metadata.DeletionTime != "" {
		return nil, secretstore.Version{}, errors.New("Vault credential readback is invalid")
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if digest != response.Data.Data.ContentDigest {
		return nil, secretstore.Version{}, errors.New("Vault credential digest mismatch")
	}
	return raw, secretstore.Version{Ref: ref, Version: response.Data.Metadata.Version, ContentDigest: digest}, nil
}

func (vault *Vault) Revoke(ctx context.Context, ref string, version uint64) error {
	if !vault.validRef(ref) || version == 0 {
		return errors.New("Vault revoke input is invalid")
	}
	token, err := vault.login(ctx)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string][]uint64{"versions": {version}})
	return vault.request(ctx, http.MethodPost, "v1/"+path.Clean(vault.config.KVMount)+"/destroy/"+path.Clean(ref), token, body, nil)
}

func (vault *Vault) Check(ctx context.Context) error {
	token, err := vault.login(ctx)
	if err != nil {
		return err
	}
	dataPath := path.Clean(vault.config.KVMount) + "/data/" + path.Clean(vault.config.PathPrefix) + "/readiness"
	body, _ := json.Marshal(map[string][]string{"paths": {dataPath}})
	var response struct {
		Capabilities []string `json:"capabilities"`
	}
	if err = vault.request(ctx, http.MethodPost, "v1/sys/capabilities-self", token, body, &response); err != nil {
		return err
	}
	for _, capability := range response.Capabilities {
		if vault.config.ReadOnly && capability == "read" || !vault.config.ReadOnly && (capability == "create" || capability == "update") {
			return nil
		}
	}
	return errors.New("Vault credential path capability is unavailable")
}

func (vault *Vault) validRef(ref string) bool {
	clean := path.Clean(ref)
	return clean == ref && strings.HasPrefix(ref, vault.config.PathPrefix+"/") && !strings.ContainsAny(ref, "\x00\r\n")
}

func (vault *Vault) dataPath(ref string) string {
	return "v1/" + path.Clean(vault.config.KVMount) + "/data/" + path.Clean(ref)
}

func (vault *Vault) request(ctx context.Context, method, relative, token string, body []byte, output any) error {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(vault.config.Address, "/")+"/"+strings.TrimLeft(relative, "/"), bytes.NewReader(body))
	if err != nil {
		return errors.New("create Vault request")
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("X-Vault-Token", token)
	}
	response, err := vault.client.Do(request)
	if err != nil {
		return errors.New("execute Vault request")
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maximumVaultResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil || len(raw) > maximumVaultResponseBytes {
		return errors.New("read Vault response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("Vault request rejected")
	}
	if output != nil && json.Unmarshal(raw, output) != nil {
		return errors.New("decode Vault response")
	}
	return nil
}
