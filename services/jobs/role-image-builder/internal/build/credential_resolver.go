package build

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

const credentialBindingSchema = "mattercodex.dev/role-image-input-credential/v1"

type credentialResolver struct {
	client                   *http.Client
	address, tokenFile, role string
}

type inputCredential struct {
	SourceRef, Username, Password string
}

func newCredentialResolver(config MaterializerConfig) (*credentialResolver, error) {
	if config.CredentialVaultAddress != "https://vault.mattercodex-system.svc:8200" ||
		config.CredentialVaultTLSServerName != "vault.mattercodex-system.svc.cluster.local" ||
		config.CredentialVaultRole != "role-image-builder-secret-resolver" {
		return nil, errors.New("role image credential resolver configuration is invalid")
	}
	ca, err := os.ReadFile(config.CredentialVaultCAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, ErrMaterialization
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, ErrMaterialization
	}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.CredentialVaultTLSServerName, RootCAs: pool}, //nolint:gosec // exact SNI, CA and TLS 1.3.
		DialContext:     (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: -1}).DialContext}
	return &credentialResolver{client: &http.Client{Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return ErrMaterialization }},
		address: config.CredentialVaultAddress, tokenFile: config.CredentialVaultTokenFile,
		role: config.CredentialVaultRole}, nil
}

func (resolver *credentialResolver) Check(ctx context.Context) error {
	token, err := resolver.login(ctx)
	if err != nil {
		return err
	}
	resolver.revoke(ctx, token)
	return nil
}

func (resolver *credentialResolver) Resolve(
	ctx context.Context,
	input *controlplanev1.RoleImageBuildInput,
) (map[string]inputCredential, error) {
	if len(input.GetBuildSecretRefs()) == 0 {
		return nil, nil
	}
	token, err := resolver.login(ctx)
	if err != nil {
		return nil, err
	}
	defer resolver.revoke(context.WithoutCancel(ctx), token)
	result := make(map[string]inputCredential, len(input.GetBuildSecretRefs()))
	for _, reference := range input.GetBuildSecretRefs() {
		path, version, ok := parseVersionedCredentialReference(reference)
		if !ok {
			return nil, ErrMaterialization
		}
		binding, err := resolver.readBinding(ctx, token, path, version)
		if err != nil || binding.Schema != credentialBindingSchema || binding.Reference != reference ||
			binding.ProjectID != input.GetProjectId() || binding.RecipeID != input.GetRecipeId() ||
			binding.RecipeVersion != input.GetRecipeVersion() || binding.RecipeGeneration != input.GetRecipeGeneration() ||
			!validInputSourceReference(input, binding.SourceRef) || binding.Username == "" || binding.Password == "" ||
			strings.ContainsAny(binding.Username, "\r\n") || strings.ContainsAny(binding.Password, "\r\n") {
			return nil, ErrMaterialization
		}
		if _, duplicate := result[binding.SourceRef]; duplicate {
			return nil, ErrMaterialization
		}
		result[binding.SourceRef] = inputCredential{SourceRef: binding.SourceRef, Username: binding.Username, Password: binding.Password}
	}
	return result, nil
}

type credentialBinding struct {
	Schema           string `json:"schema"`
	Reference        string `json:"reference"`
	ProjectID        string `json:"projectId"`
	RecipeID         string `json:"recipeId"`
	SourceRef        string `json:"sourceRef"`
	RecipeVersion    uint64 `json:"recipeVersion"`
	RecipeGeneration uint64 `json:"recipeGeneration"`
	Username         string `json:"username"`
	Password         string `json:"password"`
}

func (resolver *credentialResolver) readBinding(ctx context.Context, token, path string, version uint64) (credentialBinding, error) {
	target := resolver.address + "/v1/kv/data/mattercodex/role-image-builder/input-authority/" + path + "?version=" + strconv.FormatUint(version, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return credentialBinding{}, ErrMaterialization
	}
	request.Header.Set("X-Vault-Token", token)
	response, err := resolver.client.Do(request)
	if err != nil {
		return credentialBinding{}, ErrMaterialization
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return credentialBinding{}, ErrMaterialization
	}
	var envelope struct {
		Data struct {
			Data credentialBinding `json:"data"`
		} `json:"data"`
	}
	if err := decodeBoundedJSON(response.Body, &envelope); err != nil {
		return credentialBinding{}, err
	}
	return envelope.Data.Data, nil
}

func (resolver *credentialResolver) login(ctx context.Context) (string, error) {
	jwt, err := os.ReadFile(resolver.tokenFile)
	if err != nil || len(jwt) == 0 || len(jwt) > 16<<10 || strings.ContainsAny(string(jwt), "\r\n") {
		return "", ErrMaterialization
	}
	payload, _ := json.Marshal(map[string]string{"jwt": string(jwt), "role": resolver.role})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, resolver.address+"/v1/auth/kubernetes/login", bytes.NewReader(payload))
	if err != nil {
		return "", ErrMaterialization
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := resolver.client.Do(request)
	if err != nil {
		return "", ErrMaterialization
	}
	defer response.Body.Close()
	var envelope struct {
		Auth struct {
			ClientToken string `json:"client_token"`
			Lease       int64  `json:"lease_duration"`
		} `json:"auth"`
	}
	if response.StatusCode != http.StatusOK || decodeBoundedJSON(response.Body, &envelope) != nil ||
		envelope.Auth.ClientToken == "" || envelope.Auth.Lease < 60 || strings.ContainsAny(envelope.Auth.ClientToken, "\r\n") {
		return "", ErrMaterialization
	}
	return envelope.Auth.ClientToken, nil
}

func (resolver *credentialResolver) revoke(ctx context.Context, token string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, resolver.address+"/v1/auth/token/revoke-self", bytes.NewReader([]byte("{}")))
	if err == nil {
		request.Header.Set("X-Vault-Token", token)
		request.Header.Set("Content-Type", "application/json")
		response, callErr := resolver.client.Do(request)
		if callErr == nil {
			_ = response.Body.Close()
		}
	}
}

func decodeBoundedJSON(reader io.Reader, target any) error {
	value, err := io.ReadAll(io.LimitReader(reader, 1<<20+1))
	if err != nil || len(value) > 1<<20 || json.Unmarshal(value, target) != nil {
		return ErrMaterialization
	}
	return nil
}

func parseVersionedCredentialReference(reference string) (string, uint64, bool) {
	value := strings.TrimPrefix(reference, "vault-versioned://")
	if value == reference || len(value) < 4 || len(value) > 400 || strings.ContainsAny(value, "?# .\\\r\n\t") {
		return "", 0, false
	}
	path, rawVersion, found := strings.Cut(value, "/v")
	if !found || path == "" || strings.Contains(rawVersion, "/") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", 0, false
	}
	for _, character := range path {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '-' && character != '/' {
			return "", 0, false
		}
	}
	version, err := strconv.ParseUint(rawVersion, 10, 64)
	return path, version, err == nil && version > 0
}

func validInputSourceReference(input *controlplanev1.RoleImageBuildInput, reference string) bool {
	if reference == input.GetContextRef() {
		return true
	}
	for _, item := range input.GetPackages() {
		if reference == item.GetSourceRef() {
			return true
		}
	}
	for _, item := range input.GetTools() {
		if reference == item.GetSourceRef() {
			return true
		}
	}
	return false
}
