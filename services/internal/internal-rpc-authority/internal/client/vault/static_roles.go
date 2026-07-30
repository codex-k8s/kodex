package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const maxVaultResponseBytes = 1 << 20

const (
	minimumRotationPeriodSeconds = 5 * 60
	maximumRotationPeriodSeconds = 24 * 60 * 60
)

var vaultNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,94}[a-z0-9])$`)

type Config struct {
	Address                 string
	TLSServerName           string
	CAFile                  string
	AuthMount               string
	AuthRole                string
	ServiceAccountTokenFile string
	Timeout                 time.Duration
}

type StaticRoleClient struct {
	config Config
	client *http.Client
}

func NewStaticRoleClient(config Config) (*StaticRoleClient, error) {
	address, err := url.Parse(config.Address)
	if err != nil ||
		address.Scheme != "https" ||
		address.Host == "" ||
		address.Path != "" ||
		address.RawQuery != "" ||
		address.Fragment != "" ||
		config.TLSServerName == "" ||
		config.AuthMount != "kubernetes" ||
		!vaultNamePattern.MatchString(config.AuthRole) ||
		config.Timeout < time.Second ||
		config.Timeout > 15*time.Second {
		return nil, errors.New("invalid Vault static role client configuration")
	}
	certificatePool, err := loadCertificatePool(config.CAFile)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    certificatePool,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	return &StaticRoleClient{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Vault redirect is forbidden")
			},
		},
	}, nil
}

func (client *StaticRoleClient) Close() {
	client.client.CloseIdleConnections()
}

func (client *StaticRoleClient) VerifyStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	if len(roles) != 4 {
		return errors.New("Vault static role registered set must contain four roles")
	}
	token, err := client.login(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(roles))
	for _, expected := range roles {
		if !vaultNamePattern.MatchString(expected.Role) ||
			!vaultNamePattern.MatchString(expected.Principal) ||
			!vaultNamePattern.MatchString(expected.DatabaseName) {
			return errors.New("Vault static role name is outside the registry boundary")
		}
		if _, duplicate := seen[expected.Role]; duplicate {
			return errors.New("duplicate Vault static role")
		}
		seen[expected.Role] = struct{}{}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			client.config.Address+"/v1/database/static-roles/"+url.PathEscape(expected.Role),
			nil,
		)
		if err != nil {
			return errors.New("construct Vault static role read")
		}
		request.Header.Set("X-Vault-Token", token)
		response, err := client.client.Do(request)
		if err != nil {
			return errors.New("read Vault static role")
		}
		if err := verifyStaticRoleResponse(response, expected); err != nil {
			return err
		}
	}
	return nil
}

func (client *StaticRoleClient) login(ctx context.Context) (string, error) {
	jwt, err := readTokenFile(client.config.ServiceAccountTokenFile)
	if err != nil {
		return "", fmt.Errorf("read Vault Kubernetes auth token: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"role": client.config.AuthRole,
		"jwt":  string(jwt),
	})
	if err != nil {
		return "", errors.New("encode Vault Kubernetes login")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.config.Address+"/v1/auth/"+client.config.AuthMount+"/login",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", errors.New("construct Vault Kubernetes login")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return "", errors.New("perform Vault Kubernetes login")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return "", errors.New("Vault Kubernetes login rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return "", errors.New("Vault Kubernetes login response is invalid")
	}
	var envelope struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int64  `json:"lease_duration"`
			Renewable     bool   `json:"renewable"`
		} `json:"auth"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.Auth.ClientToken == "" ||
		len(envelope.Auth.ClientToken) > 4096 ||
		envelope.Auth.LeaseDuration < 30 ||
		envelope.Auth.LeaseDuration > 3600 ||
		!envelope.Auth.Renewable {
		return "", errors.New("Vault Kubernetes login response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("Vault Kubernetes login response is invalid")
	}
	return envelope.Auth.ClientToken, nil
}

func verifyStaticRoleResponse(
	response *http.Response,
	expected repository.VaultStaticRoleExpectation,
) error {
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return errors.New("Vault static role read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return errors.New("Vault static role response is invalid")
	}
	var envelope struct {
		Data struct {
			CredentialType   string `json:"credential_type"`
			DatabaseName     string `json:"db_name"`
			Username         string `json:"username"`
			RotationPeriod   int64  `json:"rotation_period"`
			RotationSchedule string `json:"rotation_schedule"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil {
		return errors.New("Vault static role response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Vault static role response is invalid")
	}
	if envelope.Data.CredentialType != "password" ||
		envelope.Data.DatabaseName != expected.DatabaseName ||
		envelope.Data.Username != expected.Principal ||
		envelope.Data.RotationPeriod < minimumRotationPeriodSeconds ||
		envelope.Data.RotationPeriod > maximumRotationPeriodSeconds ||
		envelope.Data.RotationSchedule != "" {
		return errors.New("Vault static role binding is invalid")
	}
	return nil
}

func loadCertificatePool(path string) (*x509.CertPool, error) {
	resolved, err := resolveMountedFile(path)
	if err != nil {
		return nil, fmt.Errorf("resolve Vault CA bundle: %w", err)
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read Vault CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("Vault CA bundle contains no certificate")
	}
	return pool, nil
}

func readTokenFile(path string) ([]byte, error) {
	resolved, err := resolveMountedFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > 16<<10 {
		return nil, errors.New("Vault Kubernetes auth token file is unsafe")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(string(raw))), nil
}

func resolveMountedFile(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == "../" {
		return "", errors.New("mounted file symlink escapes its directory")
	}
	return resolved, nil
}
