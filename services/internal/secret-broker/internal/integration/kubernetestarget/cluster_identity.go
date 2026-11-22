package kubernetestarget

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	strictjson "sigs.k8s.io/json"
)

const (
	// clusterIdentityUnknown rejects incomplete setup preflight without disclosing upstream bodies.
	// clusterIdentityUnknown отклоняет incomplete setup preflight без выдачи upstream bodies.
	clusterIdentityUnknown = "managed Kubernetes cluster identity is unknown"
	// clusterIdentityChanged rejects a different observed kube-system Namespace UID on the selected route.
	// clusterIdentityChanged отклоняет иной observed kube-system Namespace UID выбранного route.
	clusterIdentityChanged = "managed Kubernetes cluster identity has changed"
	// maximumClusterIdentityBytes bounds one private Namespace response before typed decoding.
	// maximumClusterIdentityBytes ограничивает один private Namespace response до typed decoding.
	maximumClusterIdentityBytes = 64 << 10
)

// VerifyClusterIdentity checks the operator-pinned kube-system Namespace UID over actual selected-identity TLS.
// The caller must hold current setup/operation authority; this read grants none and is not an atomic
// cluster-incarnation precondition for later effects. No alias, elevated identity or cached result is used.
// VerifyClusterIdentity проверяет operator-pinned kube-system Namespace UID через actual selected-identity TLS.
// Caller удерживает current setup/operation authority; read её не выдаёт и не является atomic
// cluster-incarnation precondition поздних effects. Alias, повышенная identity и cached result не используются.
func (credential Credential) VerifyClusterIdentity(ctx context.Context) error {
	if ctx == nil || validateTarget(credential.target) != nil {
		return errors.New(clusterIdentityUnknown)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, closeIdle, err := credential.NewClient(5 * time.Second)
	if err != nil {
		return errors.New(clusterIdentityUnknown)
	}
	defer closeIdle()
	request, err := credential.ReadRequest(ctx, ResourceAddress{Version: "v1", Resource: "namespaces", Name: "kube-system"})
	if err != nil {
		return errors.New(clusterIdentityUnknown)
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New(clusterIdentityUnknown)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return errors.New(accessDenied)
	}
	if response.StatusCode != http.StatusOK {
		return errors.New(clusterIdentityUnknown)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumClusterIdentityBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || len(raw) > maximumClusterIdentityBytes || !utf8.Valid(raw) {
		return errors.New(clusterIdentityUnknown)
	}
	var namespace corev1.Namespace
	strictErrors, err := strictjson.UnmarshalStrict(raw, &namespace)
	if err != nil || len(strictErrors) != 0 || namespace.Kind != "Namespace" || namespace.APIVersion != "v1" ||
		namespace.Name != "kube-system" || namespace.ResourceVersion == "" || namespace.DeletionTimestamp != nil ||
		namespace.Status.Phase != corev1.NamespaceActive {
		return errors.New(clusterIdentityUnknown)
	}
	if string(namespace.UID) != credential.target.ClusterUID {
		return errors.New(clusterIdentityChanged)
	}
	return nil
}
