package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

var kvDataPathPattern = regexp.MustCompile(
	`^kv/data/mattercodex/[a-z0-9][a-z0-9./_-]{14,500}[a-z0-9]$`,
)

func (client *StaticRoleClient) ReadKV2(
	ctx context.Context,
	path string,
) (repository.SecretMaterial, bool, error) {
	if !kvDataPathPattern.MatchString(path) {
		return repository.SecretMaterial{}, false, errors.New("vault KV path is outside the target registry boundary")
	}
	token, err := client.login(ctx)
	if err != nil {
		return repository.SecretMaterial{}, false, err
	}
	return client.readKV2WithToken(ctx, token, path)
}

func (client *StaticRoleClient) CreateKV2(
	ctx context.Context,
	path string,
	data map[string]string,
) (repository.SecretMaterial, error) {
	if !kvDataPathPattern.MatchString(path) ||
		len(data) == 0 ||
		len(data) > 16 {
		return repository.SecretMaterial{}, errors.New("vault KV delivery input is outside the target registry boundary")
	}
	for key, value := range data {
		if key == "" || len(key) > 64 || value == "" || len(value) > 1<<20 {
			return repository.SecretMaterial{}, errors.New("vault KV delivery field is invalid")
		}
	}
	token, err := client.login(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if existing, found, readErr := client.readKV2WithToken(ctx, token, path); readErr != nil {
		return repository.SecretMaterial{}, readErr
	} else if found {
		return existing, nil
	}
	body, err := json.Marshal(struct {
		Data    map[string]string `json:"data"`
		Options struct {
			CAS uint64 `json:"cas"`
		} `json:"options"`
	}{Data: data})
	if err != nil {
		return repository.SecretMaterial{}, errors.New("encode Vault KV delivery")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.config.Address+"/v1/"+escapeVaultPath(path),
		bytes.NewReader(body),
	)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("construct Vault KV delivery")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Vault-Token", token)
	response, err := client.client.Do(request)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("write Vault KV delivery")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK &&
		response.StatusCode != http.StatusNoContent &&
		response.StatusCode != http.StatusBadRequest {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return repository.SecretMaterial{}, errors.New("vault KV delivery rejected")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
	stored, found, err := client.readKV2WithToken(ctx, token, path)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if !found {
		return repository.SecretMaterial{}, errors.New("vault KV delivery readback is absent")
	}
	return stored, nil
}

func (client *StaticRoleClient) WriteKV2CAS(
	ctx context.Context,
	path string,
	expectedVersion uint64,
	data map[string]string,
) (repository.SecretMaterial, error) {
	if !kvDataPathPattern.MatchString(path) ||
		expectedVersion == 0 ||
		len(data) == 0 ||
		len(data) > 16 {
		return repository.SecretMaterial{}, errors.New(
			"vault KV rotation input is outside the target registry boundary",
		)
	}
	for key, value := range data {
		if key == "" || len(key) > 64 || value == "" || len(value) > 1<<20 {
			return repository.SecretMaterial{}, errors.New("vault KV rotation field is invalid")
		}
	}
	token, err := client.login(ctx)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	body, err := json.Marshal(struct {
		Data    map[string]string `json:"data"`
		Options struct {
			CAS uint64 `json:"cas"`
		} `json:"options"`
	}{
		Data: data,
		Options: struct {
			CAS uint64 `json:"cas"`
		}{CAS: expectedVersion},
	})
	if err != nil {
		return repository.SecretMaterial{}, errors.New("encode Vault KV rotation")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.config.Address+"/v1/"+escapeVaultPath(path),
		bytes.NewReader(body),
	)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("construct Vault KV rotation")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Vault-Token", token)
	response, err := client.client.Do(request)
	if err != nil {
		return repository.SecretMaterial{}, errors.New("write Vault KV rotation")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK &&
		response.StatusCode != http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return repository.SecretMaterial{}, errors.New("vault KV rotation CAS rejected")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
	stored, found, err := client.readKV2WithToken(ctx, token, path)
	if err != nil {
		return repository.SecretMaterial{}, err
	}
	if !found || stored.Version != expectedVersion+1 {
		return repository.SecretMaterial{}, errors.New("vault KV rotation readback is invalid")
	}
	return stored, nil
}

func (client *StaticRoleClient) readKV2WithToken(
	ctx context.Context,
	token string,
	path string,
) (repository.SecretMaterial, bool, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		client.config.Address+"/v1/"+escapeVaultPath(path),
		nil,
	)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("construct Vault KV readback")
	}
	request.Header.Set("X-Vault-Token", token)
	response, err := client.client.Do(request)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("read Vault KV delivery")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return repository.SecretMaterial{}, false, nil
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxVaultResponseBytes))
		return repository.SecretMaterial{}, false, errors.New("vault KV readback rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxVaultResponseBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxVaultResponseBytes {
		return repository.SecretMaterial{}, false, errors.New("vault KV readback response is invalid")
	}
	var envelope struct {
		Data struct {
			Data     map[string]string `json:"data"`
			Metadata struct {
				Version      uint64 `json:"version"`
				Destroyed    bool   `json:"destroyed"`
				DeletionTime string `json:"deletion_time"`
			} `json:"metadata"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.Data.Metadata.Version == 0 ||
		envelope.Data.Metadata.Destroyed ||
		envelope.Data.Metadata.DeletionTime != "" ||
		len(envelope.Data.Data) == 0 ||
		len(envelope.Data.Data) > 16 {
		return repository.SecretMaterial{}, false, errors.New("vault KV readback response is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return repository.SecretMaterial{}, false, errors.New("vault KV readback response is invalid")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(envelope.Data.Data)
	if err != nil {
		return repository.SecretMaterial{}, false, errors.New("digest Vault KV readback")
	}
	return repository.SecretMaterial{
		Version: envelope.Data.Metadata.Version,
		Data:    envelope.Data.Data,
		Digest:  digest,
	}, true, nil
}

func escapeVaultPath(path string) string {
	parts := strings.Split(path, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

var _ repository.SecretDelivery = (*StaticRoleClient)(nil)
