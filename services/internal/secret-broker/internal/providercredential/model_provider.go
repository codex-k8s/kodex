package providercredential

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// ConfigureModelProvider binds the optional Git-owned gateway before workers or requests start.
// ConfigureModelProvider связывает optional Git-owned gateway до запуска workers и запросов.
func (service *Service) ConfigureModelProvider(config runtimecontract.ModelProviderConfiguration) error {
	if config.Validate() != nil {
		return ErrInvalidInput
	}
	cloned, err := config.Clone()
	if err != nil {
		return err
	}
	config = cloned
	service.modelProvider = &config
	service.gatewayHTTP = newGatewayCatalogHTTPClient(config)
	return nil
}

// MaterializeGatewayAPIKey accepts only the active owner-selected definition and stores a distinct secret kind.
// MaterializeGatewayAPIKey принимает только active owner-selected definition и сохраняет отдельный secret kind.
func (service *Service) MaterializeGatewayAPIKey(ctx context.Context, attemptRef, accountRef, definitionKey string, key []byte) (kubernetesstore.ProviderCredentialDescriptor, string, error) {
	if service.modelProvider == nil || definitionKey != service.modelProvider.DefinitionKey() {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrInvalidInput
	}
	if _, _, err := deviceAuthorizationReferences(attemptRef, accountRef); err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrInvalidInput
	}
	raw, err := runtimecontract.EncodeGatewayProviderAuthentication(definitionKey, key)
	if err != nil {
		return kubernetesstore.ProviderCredentialDescriptor{}, "", ErrInvalidInput
	}
	defer clear(raw)
	descriptor, err := service.store.CreateProviderCredential(ctx, attemptRef, accountRef, raw)
	if errors.Is(err, kubernetesstore.ErrProviderCredentialConflict) {
		return descriptor, "", ErrConflict
	}
	if err != nil {
		return descriptor, "", err
	}
	return descriptor, "Gateway API key account", nil
}

// newGatewayCatalogHTTPClient retains the managed TLS proxy and closes CONNECT to the configured gateway.
// newGatewayCatalogHTTPClient сохраняет managed TLS proxy и ограничивает CONNECT configured gateway.
func newGatewayCatalogHTTPClient(config runtimecontract.ModelProviderConfiguration) *http.Client {
	client := newModelCatalogHTTPClient()
	target, _ := url.Parse(config.BaseURL)
	client.Transport.(*http.Transport).OnProxyConnectResponse = func(_ context.Context, proxy *url.URL, request *http.Request, response *http.Response) error {
		if proxy == nil || proxy.String() != providerEgressProxyURL || request == nil || request.Method != http.MethodConnect || request.Host != target.Hostname()+":443" || response == nil || response.StatusCode != http.StatusOK {
			return errors.New("gateway catalog CONNECT boundary is invalid")
		}
		return nil
	}
	return client
}

// observeGatewayCatalog intersects bounded remote availability with deployment-approved model IDs.
// Operator token ceilings are not advertised as remotely verified capabilities; reasoning stays unsupported.
// Unselected remote aliases are discarded before validating matching IDs; approved duplicates still reject the complete observation.
// observeGatewayCatalog пересекает bounded remote availability с deployment-approved model IDs.
// Operator token ceilings не выдаются за remote capabilities; reasoning остаётся unsupported.
// Невыбранные remote aliases отбрасываются до проверки совпавших IDs; повторы разрешённой модели отклоняют всё наблюдение.
func (service *Service) observeGatewayCatalog(ctx context.Context, raw []byte) (ModelCatalog, error) {
	config := service.modelProvider
	if config == nil {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	key, err := runtimecontract.DecodeGatewayProviderAuthentication(raw, config.DefinitionKey())
	if err != nil {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	defer clear(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.BaseURL+"/models", nil)
	if err != nil {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	request.Header.Set("Authorization", "Bearer "+string(key))
	request.Header.Set("Accept", "application/json")
	response, err := service.gatewayHTTP.Do(request)
	if err != nil {
		return catalogFailure(ctx, errors.New("gateway catalog request failed"))
	}
	if response == nil || response.Body == nil {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return catalogFailure(ctx, errModelCatalogAuthorization)
	}
	if response.StatusCode != http.StatusOK {
		return catalogFailure(ctx, errors.New("gateway catalog is unavailable"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumModelCatalogBytes+1))
	defer clear(body)
	if err != nil || len(body) > maximumModelCatalogBytes {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	var listing struct {
		Data *[]struct {
			ID string `json:"id"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if json.Unmarshal(body, &listing) != nil || listing.Data == nil || listing.HasMore || len(*listing.Data) > maximumAPIModelRecords {
		return catalogFailure(ctx, errModelCatalogUnverified)
	}
	approved := make(map[string]bool, len(config.Models))
	for _, model := range config.Models {
		approved[model.ID] = true
	}
	ids := make(map[string]bool, len(config.Models))
	for _, item := range *listing.Data {
		if !approved[item.ID] {
			continue
		}
		if !modelCatalogIDPattern.MatchString(item.ID) || ids[item.ID] {
			return catalogFailure(ctx, errModelCatalogUnverified)
		}
		ids[item.ID] = true
	}
	models := []CatalogModel{}
	for _, item := range config.Models {
		if ids[item.ID] {
			models = append(models, CatalogModel{ID: item.ID})
		}
	}
	return ModelCatalog{ObservedAt: time.Now().UTC(), Source: CatalogRemoteAPI, Failure: CatalogFailureNone, Models: models}, nil
}
