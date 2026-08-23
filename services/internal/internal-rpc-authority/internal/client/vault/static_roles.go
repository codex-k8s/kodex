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
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/securefile"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const maxVaultResponseBytes = 1 << 20

const (
	minimumRotationPeriodSeconds = 5 * 60
	maximumRotationPeriodSeconds = 24 * 60 * 60
)

var vaultNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,94}[a-z0-9])$`)

// Config задаёт проверенную конфигурацию клиента Vault.
type Config struct {
	Address                 string
	TLSServerName           string
	CAFile                  string
	AuthMount               string
	AuthRole                string
	ServiceAccountTokenFile string
	Timeout                 time.Duration
}

// StaticRoleClient управляет жизненным циклом статических ролей и KV v2.
type StaticRoleClient struct {
	config Config
	client *http.Client
}

// NewStaticRoleClient создаёт клиент с обязательной проверкой TLS и адреса.
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
		return nil, errors.New("invalid vault static role client configuration")
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
				return errors.New("vault redirect is forbidden")
			},
		},
	}, nil
}

// Close закрывает простаивающие соединения клиента.
func (client *StaticRoleClient) Close() {
	client.client.CloseIdleConnections()
}

// VerifyStaticRoles сверяет зарегистрированный набор статических ролей.
func (client *StaticRoleClient) VerifyStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	if len(roles) == 0 || len(roles) > 16 {
		return errors.New("vault static role registered set is outside the bounded lifecycle")
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
			return errors.New("vault static role name is outside the registry boundary")
		}
		if _, duplicate := seen[expected.Role]; duplicate {
			return errors.New("duplicate vault static role")
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

// RotateStaticRoles запускает ротацию зарегистрированных статических ролей.
func (client *StaticRoleClient) RotateStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	return client.mutateStaticRoles(ctx, roles, "rotate", http.MethodPost)
}

// RevokeStaticRoles отзывает выведенные из обращения статические роли.
func (client *StaticRoleClient) RevokeStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	return client.mutateStaticRoles(ctx, roles, "revoke", http.MethodDelete)
}

// VerifyRevokedStaticRoles подтверждает недоступность отозванных ролей.
func (client *StaticRoleClient) VerifyRevokedStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	if len(roles) == 0 {
		return nil
	}
	token, err := client.login(ctx)
	if err != nil {
		return err
	}
	for _, expected := range roles {
		if err := validateStaticRoleExpectation(expected); err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			client.config.Address+"/v1/database/static-roles/"+url.PathEscape(expected.Role),
			nil,
		)
		if err != nil {
			return errors.New("construct Vault retired static role read")
		}
		request.Header.Set("X-Vault-Token", token)
		response, err := client.client.Do(request)
		if err != nil {
			return errors.New("read Vault retired static role")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			return errors.New("vault retired static role remains reachable")
		}
	}
	return nil
}

func (client *StaticRoleClient) mutateStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
	operation string,
	method string,
) error {
	if len(roles) == 0 {
		return nil
	}
	if len(roles) > 16 {
		return errors.New("vault static role mutation set is unbounded")
	}
	token, err := client.login(ctx)
	if err != nil {
		return err
	}
	for _, expected := range roles {
		if err := validateStaticRoleExpectation(expected); err != nil {
			return err
		}
		path := "/v1/database/rotate-role/"
		if operation == "revoke" {
			path = "/v1/database/static-roles/"
		}
		request, err := http.NewRequestWithContext(
			ctx,
			method,
			client.config.Address+path+url.PathEscape(expected.Role),
			nil,
		)
		if err != nil {
			return errors.New("construct Vault static role lifecycle request")
		}
		request.Header.Set("X-Vault-Token", token)
		response, err := client.client.Do(request)
		if err != nil {
			return errors.New("perform Vault static role lifecycle request")
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK &&
			response.StatusCode != http.StatusNoContent {
			return errors.New("vault static role lifecycle request rejected")
		}
	}
	return nil
}

func validateStaticRoleExpectation(
	expected repository.VaultStaticRoleExpectation,
) error {
	if !vaultNamePattern.MatchString(expected.Role) ||
		!vaultNamePattern.MatchString(expected.Principal) ||
		!vaultNamePattern.MatchString(expected.DatabaseName) {
		return errors.New("vault static role name is outside the registry boundary")
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
		return "", errors.New("vault Kubernetes login rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return "", errors.New("vault Kubernetes login response is invalid")
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
		return "", errors.New("vault Kubernetes login response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("vault Kubernetes login response is invalid")
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
		return errors.New("vault static role read rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return errors.New("vault static role response is invalid")
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
		return errors.New("vault static role response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("vault static role response is invalid")
	}
	if envelope.Data.CredentialType != "password" ||
		envelope.Data.DatabaseName != expected.DatabaseName ||
		envelope.Data.Username != expected.Principal ||
		envelope.Data.RotationPeriod < minimumRotationPeriodSeconds ||
		envelope.Data.RotationPeriod > maximumRotationPeriodSeconds ||
		envelope.Data.RotationSchedule != "" {
		return errors.New("vault static role binding is invalid")
	}
	return nil
}

func loadCertificatePool(path string) (*x509.CertPool, error) {
	raw, err := securefile.Read(path, maxVaultResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read Vault CA bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("vault CA bundle contains no certificate")
	}
	return pool, nil
}

func readTokenFile(path string) ([]byte, error) {
	raw, err := securefile.Read(path, 16<<10)
	if err != nil {
		return nil, errors.New("vault Kubernetes auth token file is unsafe")
	}
	return []byte(strings.TrimSpace(string(raw))), nil
}
