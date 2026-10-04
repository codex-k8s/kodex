// Package runtimepolicy проверяет execution-scoped доступ agent runtime к
// публичной сети. Listener принимает только подписанный server-owned grant.
package runtimepolicy

import (
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

type Active struct {
	base *policy.Active
	key  []byte
}

func New(base *policy.Active, key []byte) (*Active, error) {
	if base == nil || len(key) < 32 {
		return nil, errors.New("runtime egress policy input is invalid")
	}
	return &Active{base: base, key: append([]byte(nil), key...)}, nil
}

func (active *Active) Allows(string, int) bool { return false }

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

func (active *Active) Limits() policy.Limits { return active.base.Limits() }

func (active *Active) Revision() string { return active.base.Revision() + "+runtime-v1" }

func (active *Active) Digest() string { return active.base.Digest() }

func (active *Active) ProfileIdentity() (string, string, string) {
	return "runtime-execution", "agent-runner", "runtime.web"
}
