package providercredential

import (
	"context"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// catalogStoreFixture replaces only exact credential reads with a programmable callback while embedding
// the broader synthetic store stub; it provides no real Kubernetes or owner authorization.
// catalogStoreFixture подменяет только точное credential чтение программируемым callback при встроенной
// общей synthetic store подмене; реальных Kubernetes и owner авторизации нет.
type catalogStoreFixture struct {
	providerCredentialStoreStub
	read func(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error)
}

// ReadProviderCredentialExact forwards account and full descriptor unchanged to the fixture callback so
// tests can verify the service performs the intended exact read.
// ReadProviderCredentialExact передаёт account и полный descriptor без изменений в fixture callback для
// проверки нужного точного чтения сервисом.
func (store *catalogStoreFixture) ReadProviderCredentialExact(ctx context.Context, account string, descriptor kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return store.read(ctx, account, descriptor)
}

// catalogAppFixture injects one model-observation callback over the synthetic app-server stub to model
// success, invalid results and lifecycle cancellation without provider access.
// catalogAppFixture внедряет один model-observation callback поверх synthetic app-server подмены для
// моделирования успеха, неверных результатов и lifecycle cancellation без provider доступа.
type catalogAppFixture struct {
	providerCredentialBackendStub
	observe func(context.Context, []byte, string) (ModelCatalog, error)
}

// ObserveModelCatalog relays context, credential bytes and method to the programmed fixture callback
// without copying or performing network operations.
// ObserveModelCatalog передаёт context, credential байты и method в запрограммированный fixture callback
// без копирования и сетевых операций.
func (server *catalogAppFixture) ObserveModelCatalog(ctx context.Context, raw []byte, method string) (ModelCatalog, error) {
	return server.observe(ctx, raw, method)
}
