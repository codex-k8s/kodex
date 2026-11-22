package kubernetes

import (
	"context"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	corev1 "k8s.io/api/core/v1"
)

// SSHIdentityStore reuses the already trusted API transport in one fixed purpose namespace without exposing its SDK identity, copying its atomics or changing the runtime-secret store's namespace.
// SSHIdentityStore переиспользует trusted API transport в одном fixed purpose namespace без выдачи SDK identity, копирования atomics либо изменения namespace runtime-secret store.
func (store *Store) SSHIdentityStore() (*Store, error) {
	if store == nil || store.client == nil {
		return nil, sshrepository.ErrIdentityUnavailable
	}
	return New(store.client, "kodex-ssh-identities")
}

// CheckSSHIdentityStorage requires the real fixed namespace to be active and checks Secret GET permission through the same private transport. It creates no probe, never treats a missing namespace as readiness and does not claim CREATE authorization or external SSH authentication.
// CheckSSHIdentityStorage требует active fixed namespace и проверяет Secret GET permission тем же private transport. Probe не создаётся; missing namespace не считается readiness, CREATE authorization либо SSH authentication не заявляются.
func (store *Store) CheckSSHIdentityStorage(ctx context.Context) error {
	if store == nil || store.namespace != "kodex-ssh-identities" || ctx == nil || ctx.Err() != nil {
		return sshrepository.ErrIdentityUnavailable
	}
	client, err := store.sshIdentityRESTClient()
	if err != nil {
		return sshrepository.ErrIdentityUnavailable
	}
	operation, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if limiter := client.GetRateLimiter(); limiter != nil {
		if err := limiter.Wait(operation); err != nil {
			return sshrepository.ErrIdentityUnavailable
		}
	}
	body := newPrivateIdentityRequestBody(nil)
	defer body.Close()
	target := client.Get().Resource("namespaces").Name(store.namespace).URL()
	response, err := sshIdentitySendHTTP(operation, client, http.MethodGet, target, body, 0)
	defer clear(response.body)
	if err != nil || operation.Err() != nil || response.status != http.StatusOK || !response.json {
		return sshrepository.ErrIdentityUnavailable
	}
	var namespace corev1.Namespace
	if internalrpcauth.DecodeStrictJSON(response.body, &namespace) != nil || namespace.APIVersion != "v1" || namespace.Kind != "Namespace" || namespace.Name != store.namespace || namespace.UID == "" || namespace.ResourceVersion == "" || namespace.DeletionTimestamp != nil || namespace.Status.Phase != corev1.NamespaceActive {
		return sshrepository.ErrIdentityUnavailable
	}
	probe, err := store.sshIdentityHTTP(operation, client, http.MethodGet, "ssh-identity-readiness-probe", nil)
	defer clear(probe.body)
	if err != nil || operation.Err() != nil || probe.status != http.StatusNotFound {
		return sshrepository.ErrIdentityUnavailable
	}
	return nil
}
