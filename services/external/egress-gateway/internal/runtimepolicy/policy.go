// Package runtimepolicy verifies execution-scoped network access for agent runtimes.
// Package runtimepolicy проверяет доступ среды агента к сети в рамках запуска.
// Its listener accepts only a signed, server-owned grant.
// Его вход принимает только подписанный grant, назначенный сервером.
package runtimepolicy

import (
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// Active binds the immutable base policy to an isolated runtime grant key.
// Active связывает неизменяемую базовую политику с отдельным ключом runtime grant.
type Active struct {
	base *policy.Active
	key  []byte
}

// New copies a sufficiently long grant key and rejects missing base policy.
// New копирует достаточно длинный ключ grant и отклоняет отсутствие базовой политики.
func New(base *policy.Active, key []byte) (*Active, error) {
	if base == nil || len(key) < 32 {
		return nil, errors.New("runtime egress policy input is invalid")
	}
	return &Active{base: base, key: append([]byte(nil), key...)}, nil
}

// Allows denies unauthenticated runtime CONNECT requests without exceptions.
// Allows без исключений отклоняет runtime CONNECT без аутентификации.
func (active *Active) Allows(string, int) bool { return false }

// AuthorizeAuthenticated verifies the grant and admits only provider or explicit web destinations.
// AuthorizeAuthenticated проверяет grant и допускает только адресаты провайдера или явного web-доступа.
func (active *Active) AuthorizeAuthenticated(hostname string, port int, credential string) (runtimecontract.RuntimeProxyAccess, bool) {
	if port != 443 {
		return runtimecontract.RuntimeProxyAccess{}, false
	}
	claims, err := runtimecontract.VerifyRuntimeWebAccessGrant(active.key, credential)
	if err != nil || claims.WorkloadRef == "" || claims.NetworkDigest == strings.Repeat("0", 64) {
		return runtimecontract.RuntimeProxyAccess{}, false
	}
	access := runtimecontract.RuntimeProxyAccess{WebAccess: claims.WebAccess,
		ProviderAccess: runtimecontract.RuntimeProviderAllowsHost(hostname) && active.base.Allows(hostname, port)}
	return access, access.ProviderAccess || runtimecontract.RuntimeWebAccessAllowsHost(claims.WebAccess, hostname)
}

// Limits returns the base policy's connection and request bounds.
// Limits возвращает лимиты соединений и запросов базовой политики.
func (active *Active) Limits() policy.Limits { return active.base.Limits() }

// Revision distinguishes runtime grant policy from the base policy revision.
// Revision отличает политику runtime grant от версии базовой политики.
func (active *Active) Revision() string { return active.base.Revision() + "+runtime-v1" }

// Digest returns the exact digest of the immutable base policy.
// Digest возвращает точный дайджест неизменяемой базовой политики.
func (active *Active) Digest() string { return active.base.Digest() }

// ProfileIdentity identifies the server-selected runtime listener profile.
// ProfileIdentity обозначает выбранный сервером профиль runtime-входа.
func (active *Active) ProfileIdentity() (string, string, string) {
	return "runtime-execution", "agent-runner", "runtime.web"
}
