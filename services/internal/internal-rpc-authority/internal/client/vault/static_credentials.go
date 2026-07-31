package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

// ReadStaticCredentialDigests читает фактически обслуживаемые static-creds,
// но возвращает только односторонний digest; пароль не покидает adapter.
func (client *StaticRoleClient) ReadStaticCredentialDigests(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) (map[string]string, error) {
	if len(roles) == 0 || len(roles) > 8 {
		return nil, errors.New("vault static credential readback set is invalid")
	}
	token, err := client.login(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(roles))
	for _, expected := range roles {
		if err := validateStaticRoleExpectation(expected); err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			client.config.Address+"/v1/database/static-creds/"+
				url.PathEscape(expected.Role),
			nil,
		)
		if err != nil {
			return nil, errors.New("construct Vault static credential readback")
		}
		request.Header.Set("X-Vault-Token", token)
		response, err := client.client.Do(request)
		if err != nil {
			return nil, errors.New("read Vault static credential")
		}
		digest, err := staticCredentialDigest(response, expected)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[expected.Role]; duplicate {
			return nil, errors.New("duplicate Vault static credential role")
		}
		result[expected.Role] = digest
	}
	return result, nil
}

func staticCredentialDigest(
	response *http.Response,
	expected repository.VaultStaticRoleExpectation,
) (string, error) {
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(
			io.Discard,
			io.LimitReader(response.Body, maxVaultResponseBytes),
		)
		return "", errors.New("vault static credential readback rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return "", errors.New("vault static credential response is invalid")
	}
	var envelope struct {
		Data struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.Data.Username != expected.Principal ||
		len(envelope.Data.Password) < 16 ||
		len(envelope.Data.Password) > 4096 {
		return "", errors.New("vault static credential binding is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("vault static credential response is invalid")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Role      string `json:"role"`
		Principal string `json:"principal"`
		Password  string `json:"password"`
	}{
		Role:      expected.Role,
		Principal: envelope.Data.Username,
		Password:  envelope.Data.Password,
	})
	if err != nil {
		return "", errors.New("digest Vault static credential readback")
	}
	return digest, nil
}
