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
