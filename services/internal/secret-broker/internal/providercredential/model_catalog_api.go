package providercredential

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	providerEgressProxyURL = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
	providerModelsURL      = "https://api.openai.com/v1/models"
	maximumAPIModelRecords = 4096
)

type modelCatalogHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func newModelCatalogHTTPClient() *http.Client {
	proxy, _ := url.Parse(providerEgressProxyURL)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(proxy),
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			DisableKeepAlives:     true,
			OnProxyConnectResponse: func(_ context.Context, actual *url.URL, request *http.Request, response *http.Response) error {
				if actual == nil || actual.String() != providerEgressProxyURL || request == nil || request.Method != http.MethodConnect || request.Host != "api.openai.com:443" || response == nil || response.StatusCode != http.StatusOK {
					return errors.New("provider catalog CONNECT boundary is invalid")
				}
				return nil
			},
		},
		Timeout: modelCatalogTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("provider catalog redirect is rejected")
		},
	}
}

// API не сообщает reasoning: допускаются только runtime capabilities,
// пересечённые с exact account list через защищённый HTTPS путь.
func readAPIModelCatalog(ctx context.Context, client modelCatalogHTTPClient, apiKey []byte, capabilities []CatalogModel) ([]CatalogModel, error) {
	if client == nil || len(apiKey) < minimumAPIKeyBytes || len(apiKey) > maximumAPIKeyBytes || validateCatalogModels(capabilities) != nil {
		return nil, errModelCatalogUnverified
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, providerModelsURL, nil)
	if err != nil {
		return nil, errors.New("create provider catalog request")
	}
	request.Header.Set("Authorization", "Bearer "+string(apiKey))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "kodex-secret-broker")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("provider catalog request failed")
	}
	if response == nil || response.Body == nil {
		return nil, errModelCatalogUnverified
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return nil, errModelCatalogAuthorization
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("provider catalog is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumModelCatalogBytes+1))
	if err != nil || len(raw) > maximumModelCatalogBytes {
		return nil, errModelCatalogUnverified
	}
	defer clear(raw)
	var result struct {
		Object string `json:"object"`
		Data   *[]struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Object != "list" || result.Data == nil || result.HasMore || len(*result.Data) > maximumAPIModelRecords {
		return nil, errModelCatalogUnverified
	}
	ids := make(map[string]struct{}, len(*result.Data))
	for _, model := range *result.Data {
		if !modelCatalogIDPattern.MatchString(model.ID) || model.Object != "model" {
			return nil, errModelCatalogUnverified
		}
		if _, duplicate := ids[model.ID]; duplicate {
			return nil, errModelCatalogUnverified
		}
		ids[model.ID] = struct{}{}
	}
	models := make([]CatalogModel, 0, len(capabilities))
	for _, model := range capabilities {
		if _, available := ids[model.ID]; available {
			models = append(models, model)
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return models, nil
}
