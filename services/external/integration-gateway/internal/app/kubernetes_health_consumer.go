package app

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// newKubernetesHealthConsumer uses the existing synchronous purpose probe independently of resource INPUT, OUTPUT, stream readiness or an execution runner. Its lifecycle owns no asynchronous work: the common worker must join every probe before dependencies close.
// newKubernetesHealthConsumer использует existing synchronous purpose probe независимо от resource INPUT, OUTPUT, stream readiness и execution runner. Асинхронной работы нет: common worker обязан присоединить каждый probe до закрытия dependencies.
func newKubernetesHealthConsumer(lifecycle context.Context, instance string, adapter *integration.Adapter, broker sb.ManagedKubernetesServiceClient) (*kubernetesConsumer, error) {
	return newKubernetesPurposeConsumer(lifecycle, instance, 1, adapter, broker, nil)
}

// integrationGatewayHealthBrokerOperations grants only the registered health execution method alongside the unchanged SSH Compose projection. It adds neither stream exchange, cancel nor readiness permission and creates a fresh closed map on every call.
// integrationGatewayHealthBrokerOperations добавляет только registered health execution method рядом с прежней SSH Compose projection. Stream exchange, cancel и readiness permissions не добавляются; каждый вызов создаёт новую закрытую map.
func integrationGatewayHealthBrokerOperations() map[string]string {
	operations := controlplaneclient.SSHComposeCredentialProjectionOperations()
	operations["kubernetes.execute"] = sb.ManagedKubernetesService_ExecuteManagedKubernetesOperation_FullMethodName
	return operations
}

// newKubernetesHealthRegistry composes only the protected purpose health client and the typed owner delivery probe; execution INPUT/OUTPUT and stream readiness cannot enable local invocation capacity.
// newKubernetesHealthRegistry соединяет лишь protected purpose health client и typed owner delivery probe; execution INPUT/OUTPUT и stream readiness не включают local invocation capacity.
func newKubernetesHealthRegistry(lifecycle context.Context, instance string, adapter *integration.Adapter, broker sb.ManagedKubernetesServiceClient, owner cp.ManagedKubernetesOwnerServiceClient) (*brokerIdentityConsumers, error) {
	health, err := newKubernetesHealthConsumer(lifecycle, instance, adapter, broker)
	if err != nil {
		return nil, err
	}
	return &brokerIdentityConsumers{Kubernetes: health, deliveryContract: kubernetesDeliveryContract(owner)}, nil
}
