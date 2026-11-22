package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// BoundPodStreamAccess rechecks one admitted Pod transport without opening or replacing it.
// Credentials are freshly loaded by exact purpose locator and never retained in this verifier.
// BoundPodStreamAccess перепроверяет один admitted Pod transport без открытия либо замены.
// Credentials свежо читаются по exact purpose locator и не сохраняются в verifier.
type BoundPodStreamAccess struct {
	reader                             targetrepo.IdentityMaterialReader
	identity                           value.KubernetesIdentityLocator
	subject, metadataDigest, operation string
	pin                                PrivilegedTransportPin
	pod                                PodStreamTarget
	options                            PodStreamOptions
	protected                          []string
}

// String conceals the private reader and owner metadata during diagnostics.
// String скрывает private reader и owner metadata при диагностике.
func (*BoundPodStreamAccess) String() string { return "[managed Kubernetes stream authorization]" }

// GoString applies the same boundary to detailed Go formatting.
// GoString сохраняет ту же границу для подробного Go formatting.
func (access *BoundPodStreamAccess) GoString() string { return access.String() }

// NewBoundPodStreamAccess captures trusted child coordinates after owner admission, without I/O.
// It does not infer authorization from a target pin or from caller-supplied resource names.
// NewBoundPodStreamAccess закрепляет trusted child coordinates после owner admission без I/O.
// Не выводит authorization из target pin либо caller-supplied resource names.
func NewBoundPodStreamAccess(reader targetrepo.IdentityMaterialReader, initial value.KubernetesOperationSnapshot, operation string, pod PodStreamTarget, options PodStreamOptions) (*BoundPodStreamAccess, error) {
	if reader == nil || initial.NodeTransport == nil || initial.Identity.Validate(initial.Identity.Namespace) != nil || initial.Identity.TargetRef != initial.Target.ID || !managedkubernetes.ValidDigest(initial.SubjectDigest) || initial.IdentityRef != initial.Identity.IdentityRef {
		return nil, errors.New(privilegedTransportUnavailable)
	}
	pin := podStreamPin(*initial.NodeTransport)
	probe := Credential{target: initial.Target}
	if _, _, err := probe.StreamRequest(context.Background(), pin, time.Now(), operation, pod, options); err != nil {
		return nil, errors.New(privilegedTransportUnavailable)
	}
	digest, err := podStreamMetadataDigest(initial.Target, initial.NodeTransport)
	if err != nil {
		return nil, errors.New(privilegedTransportUnavailable)
	}
	options.Command = slices.Clone(options.Command)
	return &BoundPodStreamAccess{reader: reader, identity: initial.Identity, subject: initial.SubjectDigest, metadataDigest: digest, operation: operation, pin: pin, pod: pod, options: options, protected: slices.Clone(initial.ProtectedNamespaces)}, nil
}

// podStreamPin owns copies of public trust and address metadata used by the existing native transport.
// podStreamPin владеет копиями public trust и address metadata существующего native transport.
func podStreamPin(node value.KubernetesNodeTransport) PrivilegedTransportPin {
	return PrivilegedTransportPin{Ref: node.Ref, TargetID: node.TargetRef, NodeName: node.NodeName, NodeUID: node.NodeUID, ServerURL: node.ServerOrigin, TLSServerName: node.TLSServerName, CertificateAuthorityHash: node.CertificateAuthoritySHA256, ServerPublicKeyHash: node.ServerPublicKeySHA256, CertificateAuthorityPEM: bytes.Clone(node.CertificateAuthorityPEM), AddressPins: slices.Clone(node.AddressIPs), TargetRevision: uint64(node.TargetRevision), Generation: uint64(node.Generation), NotBefore: node.NotBefore, ExpiresAt: node.ExpiresAt, Protocol: node.Protocol}
}

// podStreamMetadataDigest binds operator metadata while excluding private credential bytes.
// podStreamMetadataDigest связывает operator metadata без private credential bytes.
func podStreamMetadataDigest(target Target, node *value.KubernetesNodeTransport) (string, error) {
	if node == nil || node.TargetRevision < 1 || node.Generation < 1 {
		return "", errors.New(privilegedTransportUnavailable)
	}
	raw, err := json.Marshal(struct {
		Target Target
		Node   *value.KubernetesNodeTransport
	}{target, node})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	clear(raw)
	return hex.EncodeToString(digest[:]), nil
}

// VerifyKubernetesStreamAccess repeats selected-identity TLS, current subject/RBAC and protected-Pod checks.
// The returned short credit is bounded by owner, node and certificate validity; opaque token validity
// is observed through the API, never guessed from token text or substituted from the environment.
// VerifyKubernetesStreamAccess повторяет selected-identity TLS, current subject/RBAC и protected-Pod checks.
// Short credit ограничен owner, node и certificate validity; opaque token validity проверяется API,
// не угадывается по token text и не заменяется credentials из environment.
func (access *BoundPodStreamAccess) VerifyKubernetesStreamAccess(ctx context.Context, current value.KubernetesOperationSnapshot) (time.Time, error) {
	_, deadline, err := access.authorizedCredential(ctx, current)
	return deadline, err
}

// authorizedCredential keeps a freshly checked identity inside the native adapter for one admitted dial.
// Its deadline is only short authorization credit, never the full stream lifetime.
// authorizedCredential оставляет свежо проверенную identity внутри native adapter для одного admitted dial.
// Deadline является лишь коротким authorization credit, не полным lifetime stream.
func (access *BoundPodStreamAccess) authorizedCredential(ctx context.Context, current value.KubernetesOperationSnapshot) (Credential, time.Time, error) {
	denied := errors.New(privilegedTransportUnavailable)
	if ctx == nil || ctx.Err() != nil || access == nil {
		return Credential{}, time.Time{}, denied
	}
	digest, err := podStreamMetadataDigest(current.Target, current.NodeTransport)
	if err != nil || digest != access.metadataDigest || current.Identity != access.identity || current.SubjectDigest != access.subject || !slices.Equal(current.ProtectedNamespaces, access.protected) {
		return Credential{}, time.Time{}, denied
	}
	now := time.Now()
	if !current.ExpiresAt.After(now) || access.pin.ValidateFor(current.Target, now) != nil {
		return Credential{}, time.Time{}, denied
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	credential, err := LoadCredential(check, access.reader, access.identity, current.Target, now)
	if err != nil {
		return Credential{}, time.Time{}, denied
	}
	deadline, err := podStreamCredentialDeadline(credential, access.pin, now)
	if err != nil {
		return Credential{}, time.Time{}, denied
	}
	if current.ExpiresAt.Before(deadline) {
		deadline = current.ExpiresAt
	}
	checkCredit, finish := context.WithDeadline(check, deadline)
	defer finish()
	_, requirements, err := credential.StreamRequest(checkCredit, access.pin, now, access.operation, access.pod, access.options)
	if err != nil || credential.VerifyClusterIdentity(checkCredit) != nil || credential.Authorize(checkCredit, access.subject, requirements) != nil {
		return Credential{}, time.Time{}, denied
	}
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespace: access.pod.Namespace, Name: access.pod.Name, UID: access.pod.UID, Namespaced: true}
	var object map[string]any
	if credential.readResourcePolicyObject(checkCredit, access.subject, address, &object) != nil || credential.ValidateQualifiedResourcePolicy(checkCredit, access.operation, address, "Pod", "", access.subject, object, access.protected) != nil || !access.matchesCurrentPod(object) || checkCredit.Err() != nil || !deadline.After(time.Now()) {
		return Credential{}, time.Time{}, denied
	}
	return credential, deadline, nil
}

// matchesCurrentPod checks incarnation, current placement and the exact approved container without pinning mutable status RV.
// matchesCurrentPod проверяет incarnation, current placement и exact approved container без закрепления mutable status RV.
func (access *BoundPodStreamAccess) matchesCurrentPod(object map[string]any) bool {
	return matchesCurrentStreamPod(object, access.pod, access.operation)
}

// matchesCurrentStreamPod shares incarnation/container checks between API logs and pinned privileged streams.
// matchesCurrentStreamPod объединяет проверки incarnation/container для API logs и pinned privileged streams.
func matchesCurrentStreamPod(object map[string]any, pod PodStreamTarget, operation string) bool {
	metadata, ok := object["metadata"].(map[string]any)
	if !ok || metadata["uid"] != pod.UID || metadata["deletionTimestamp"] != nil {
		return false
	}
	if rv, ok := metadata["resourceVersion"].(string); !ok || rv == "" {
		return false
	}
	spec, ok := object["spec"].(map[string]any)
	if !ok || spec["nodeName"] != pod.NodeName {
		return false
	}
	if operation == "kubernetes.port-forward" {
		return true
	}
	matches := 0
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		containers, _ := spec[field].([]any)
		for _, entry := range containers {
			container, _ := entry.(map[string]any)
			if container["name"] == pod.Container {
				matches++
			}
		}
	}
	return matches == 1
}

// podStreamCredentialDeadline bounds observable credential validity without asserting an opaque token expiry.
// podStreamCredentialDeadline ограничивает наблюдаемую credential validity без утверждений об opaque token expiry.
func podStreamCredentialDeadline(credential Credential, pin PrivilegedTransportPin, now time.Time) (time.Time, error) {
	deadline, err := credentialOutputDeadline(credential, now, pin.ExpiresAt, pin.CertificateAuthorityPEM)
	if err != nil {
		return time.Time{}, errors.New(privilegedTransportUnavailable)
	}
	return deadline, nil
}
