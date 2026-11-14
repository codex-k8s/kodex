package app

import (
	"context"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/telegram"
)

// interactionExecutor routes only typed provider health checks while preserving the previous Mattermost invocation boundary and excluding Telegram from generic MCP sends.
// interactionExecutor направляет лишь typed provider health checks, сохраняя прежнюю границу Mattermost invocation и исключая Telegram из generic MCP sends.
type interactionExecutor struct {
	mattermost *mattermost.Adapter
	telegram   *telegram.Adapter
}

// Execute preserves Mattermost's existing approved invocation lifecycle; no Telegram operation can enter this method.
// Execute сохраняет прежний approved invocation lifecycle Mattermost; ни одна Telegram operation не входит в этот метод.
func (executor interactionExecutor) Execute(ctx context.Context, claim *cp.IntegrationInvocationClaim) (*cp.IntegrationEffectReceipt, error) {
	if claim.GetDefinitionKey() != "mattermost" || executor.mattermost == nil {
		return nil, errDeliveryResponse
	}
	return executor.mattermost.Execute(ctx, claim)
}

// TestConnection selects one exact registered system adapter without fallback to another provider after rejection.
// TestConnection выбирает один точный зарегистрированный системный адаптер без fallback к иному провайдеру после отказа.
func (executor interactionExecutor) TestConnection(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (string, error) {
	if claim.GetDefinitionKey() == "telegram" && executor.telegram != nil {
		return executor.telegram.TestConnection(ctx, claim)
	}
	if claim.GetDefinitionKey() == "mattermost" && executor.mattermost != nil {
		return executor.mattermost.TestConnection(ctx, claim)
	}
	return "", errDeliveryResponse
}
