package integration

import (
	"context"
	"net/url"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// configuredProviderJSON forwards the same configured origin/auth fields and caller-selected HTTP operation to the provider boundary.
// It performs no scope inference, credential substitution, retry or extra validation; provider-specific callers and callProvider retain those checks.
// configuredProviderJSON передаёт прежние настроенные origin и auth-поля и выбранную HTTP-операцию на границу провайдера.
// Он не выводит область доступа, не подменяет реквизиты, не повторяет вызов и не добавляет проверки; ими по-прежнему владеют вызывающий провайдер и callProvider.
func (adapter *Adapter) configuredProviderJSON(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, method, path string, query url.Values, body any, effectKey string) ([]byte, error) {
	return adapter.callProvider(ctx, providerCall{
		BaseURL: configuration["base_url"], Method: method, Path: path, Query: query, Body: body,
		AuthScheme: configuration["auth_scheme"], Username: configuration["username"], Credential: request.Credential,
		EffectKey: effectKey, Capability: capability,
	})
}
