package providercredential

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	CatalogMethodAPIKey     = "API_KEY"
	CatalogMethodDeviceCode = "DEVICE_CODE"
)

func (process *AppServerProcess) ObserveModelCatalog(ctx context.Context, authJSON []byte, method string) (result ModelCatalog, resultErr error) {
	if ctx == nil || len(authJSON) < 1 || len(authJSON) > maximumAuthJSONBytes {
		return ModelCatalog{}, ErrInvalidInput
	}
	auth, err := catalogAuthentication(authJSON, method)
	if err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageAuthentication, err))
	}
	defer clear(auth.apiKey)
	ctx, cancel := context.WithTimeout(ctx, modelCatalogTimeout)
	defer cancel()
	if err := process.Check(ctx); err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageRuntime, err))
	}
	if method == CatalogMethodAPIKey {
		models, err := readAPIModelCatalog(ctx, process.catalogHTTP, auth.apiKey)
		if err != nil {
			return catalogFailure(ctx, atCatalogStage(catalogStageAPI, err))
		}
		return ModelCatalog{ObservedAt: time.Now().UTC(), Source: CatalogRemoteAPI, Models: models, Failure: CatalogFailureNone}, nil
	}
	home, err := os.MkdirTemp(process.root, "catalog-")
	if err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageProcess, errors.New("create model catalog state directory")))
	}
	defer func() {
		if err := os.RemoveAll(home); err != nil {
			result, resultErr = catalogFailure(ctx, atCatalogStage(catalogStageCleanup, errors.New("remove model catalog state directory")))
		}
	}()
	started := time.Now().UTC()
	server, err := startAppServer(process.binary, home)
	if err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageProcess, err))
	}
	stopWatch, watched := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watched)
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
		case <-stopWatch:
		}
	}()
	defer func() {
		close(stopWatch)
		<-watched
		if err := server.terminate(); err != nil {
			result, resultErr = catalogFailure(ctx, atCatalogStage(catalogStageCleanup, err))
		}
	}()
	if _, err := server.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "kodex-secret-broker", "title": "Kodex secret-broker", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": method == CatalogMethodDeviceCode},
	}); err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageInitialize, err))
	}
	if err := server.write(map[string]any{"method": "initialized"}); err != nil {
		return catalogFailure(ctx, atCatalogStage(catalogStageInitialize, err))
	}
	if method == CatalogMethodDeviceCode {
		// Наблюдение получает только access token: обновление OAuth остаётся у владельца credential.
		raw, err := server.call(ctx, "account/login/start", map[string]string{
			"type": "chatgptAuthTokens", "accessToken": auth.accessToken, "chatgptAccountId": auth.accountID,
		})
		if err != nil {
			return catalogFailure(ctx, atCatalogStage(catalogStageLogin, err))
		}
		var login struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &login) != nil || login.Type != "chatgptAuthTokens" {
			return catalogFailure(ctx, atCatalogStage(catalogStageLogin, errModelCatalogUnverified))
		}
	}
	models, err := readAppServerCatalog(ctx, server)
	if err != nil {
		return catalogFailure(ctx, err)
	}
	models, err = readRemoteCodexCatalog(home, started, models)
	if err != nil {
		return catalogFailure(ctx, err)
	}
	if ctx.Err() != nil {
		return ModelCatalog{}, ctx.Err()
	}
	return ModelCatalog{ObservedAt: time.Now().UTC(), Source: CatalogRemoteCodex, Models: models, Failure: CatalogFailureNone}, nil
}

type catalogAuth struct {
	apiKey      []byte
	accessToken string
	accountID   string
}

func catalogAuthentication(raw []byte, method string) (catalogAuth, error) {
	var auth struct {
		Mode   string `json:"auth_mode"`
		APIKey string `json:"OPENAI_API_KEY"`
		Tokens *struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(raw, &auth) != nil {
		return catalogAuth{}, errModelCatalogAuthorization
	}
	switch method {
	case CatalogMethodAPIKey:
		if auth.Mode != "apikey" || auth.Tokens != nil || len(auth.APIKey) < minimumAPIKeyBytes || len(auth.APIKey) > maximumAPIKeyBytes || strings.TrimSpace(auth.APIKey) != auth.APIKey || strings.ContainsAny(auth.APIKey, "\r\n\x00") {
			return catalogAuth{}, errModelCatalogAuthorization
		}
		return catalogAuth{apiKey: []byte(auth.APIKey)}, nil
	case CatalogMethodDeviceCode:
		if auth.Mode != "chatgpt" || auth.APIKey != "" || auth.Tokens == nil || !validCatalogTokenField(auth.Tokens.AccessToken, 64<<10) || !validCatalogTokenField(auth.Tokens.AccountID, 512) {
			return catalogAuth{}, errModelCatalogAuthorization
		}
		return catalogAuth{accessToken: auth.Tokens.AccessToken, accountID: auth.Tokens.AccountID}, nil
	default:
		return catalogAuth{}, errModelCatalogAuthorization
	}
}

func validCatalogTokenField(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func readAppServerCatalog(ctx context.Context, server *appServer) ([]CatalogModel, error) {
	models := make([]CatalogModel, 0)
	cursor := ""
	seenCursors := map[string]bool{}
	for page := 0; page < 8; page++ {
		params := map[string]any{"limit": 32, "includeHidden": true}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := server.call(ctx, "model/list", params)
		if err != nil {
			return nil, atCatalogStage(catalogStageListCall, err)
		}
		var result struct {
			Data *[]struct {
				ID      string `json:"id"`
				Model   string `json:"model"`
				Default string `json:"defaultReasoningEffort"`
				Efforts []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Data == nil || len(*result.Data) > 32 {
			return nil, atCatalogStage(catalogStageListSchema, errModelCatalogUnverified)
		}
		for _, item := range *result.Data {
			if item.Model != item.ID {
				return nil, atCatalogStage(catalogStageListIdentity, errModelCatalogUnverified)
			}
			model := CatalogModel{ID: item.Model, DefaultReasoningEffort: item.Default}
			for _, effort := range item.Efforts {
				model.ReasoningEfforts = append(model.ReasoningEfforts, effort.Effort)
			}
			if len(model.ReasoningEfforts) == 0 && model.DefaultReasoningEffort == "none" {
				model.DefaultReasoningEffort = ""
			}
			models = append(models, model)
		}
		if validateCatalogModels(models) != nil {
			return nil, atCatalogStage(catalogStageListCapabilities, errModelCatalogUnverified)
		}
		if result.NextCursor == nil {
			return models, nil
		}
		cursor = *result.NextCursor
		if cursor == "" || len(cursor) > 1024 || seenCursors[cursor] {
			return nil, atCatalogStage(catalogStageListCursor, errModelCatalogUnverified)
		}
		seenCursors[cursor] = true
	}
	return nil, atCatalogStage(catalogStageListCursor, errModelCatalogUnverified)
}

func readRemoteCodexCatalog(home string, started time.Time, capabilities []CatalogModel) ([]CatalogModel, error) {
	file, err := os.OpenFile(filepath.Join(home, "models_cache.json"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, atCatalogStage(catalogStageCacheOpen, errModelCatalogUnverified)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximumModelCatalogBytes {
		return nil, atCatalogStage(catalogStageCacheMetadata, errModelCatalogUnverified)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return nil, atCatalogStage(catalogStageCacheMetadata, errModelCatalogUnverified)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumModelCatalogBytes+1))
	if err != nil || len(raw) > maximumModelCatalogBytes {
		return nil, atCatalogStage(catalogStageCacheRead, errModelCatalogUnverified)
	}
	defer clear(raw)
	return parseRemoteCodexCatalog(raw, started, time.Now().UTC(), capabilities)
}

func parseRemoteCodexCatalog(raw []byte, started, now time.Time, capabilities []CatalogModel) ([]CatalogModel, error) {
	if validateCatalogModels(capabilities) != nil {
		return nil, atCatalogStage(catalogStageListCapabilities, errModelCatalogUnverified)
	}
	var snapshot struct {
		FetchedAt     time.Time `json:"fetched_at"`
		ClientVersion string    `json:"client_version"`
		Models        *[]struct {
			Slug    string  `json:"slug"`
			Default *string `json:"default_reasoning_level"`
			Efforts []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if len(raw) > maximumModelCatalogBytes || json.Unmarshal(raw, &snapshot) != nil || snapshot.Models == nil || len(*snapshot.Models) > maximumCatalogModels {
		return nil, atCatalogStage(catalogStageCacheSchema, errModelCatalogUnverified)
	}
	if snapshot.ClientVersion != catalogCodexVersion {
		return nil, atCatalogStage(catalogStageCacheVersion, errModelCatalogUnverified)
	}
	if snapshot.FetchedAt.Before(started) || snapshot.FetchedAt.After(now) {
		return nil, atCatalogStage(catalogStageCacheFreshness, errModelCatalogUnverified)
	}
	ids := map[string]CatalogModel{}
	for _, model := range *snapshot.Models {
		if _, duplicate := ids[model.Slug]; !modelCatalogIDPattern.MatchString(model.Slug) || duplicate {
			return nil, atCatalogStage(catalogStageCacheIdentity, errModelCatalogUnverified)
		}
		remote := CatalogModel{ID: model.Slug}
		if model.Default != nil {
			remote.DefaultReasoningEffort = *model.Default
		} else if len(model.Efforts) > 0 {
			remote.DefaultReasoningEffort = "none"
		}
		for _, effort := range model.Efforts {
			remote.ReasoningEfforts = append(remote.ReasoningEfforts, effort.Effort)
		}
		if len(remote.ReasoningEfforts) == 0 && remote.DefaultReasoningEffort == "none" {
			remote.DefaultReasoningEffort = ""
		}
		if validateCatalogModels([]CatalogModel{remote}) != nil {
			return nil, atCatalogStage(catalogStageCacheCapabilities, errModelCatalogUnverified)
		}
		ids[model.Slug] = remote
	}
	models := make([]CatalogModel, 0, len(capabilities))
	for _, model := range capabilities {
		if remote, present := ids[model.ID]; present {
			if remote.DefaultReasoningEffort != model.DefaultReasoningEffort || !slices.Equal(remote.ReasoningEfforts, model.ReasoningEfforts) {
				return nil, atCatalogStage(catalogStageCapabilitiesMatch, errModelCatalogUnverified)
			}
			models = append(models, model)
		}
	}
	return models, validateCatalogModels(models)
}
