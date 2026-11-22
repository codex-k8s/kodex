package kubernetestarget

import (
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// boundLogsAccess retains only the admitted Pod/identity coordinates, never a kubelet pin or credentials.
// boundLogsAccess хранит только admitted Pod/identity coordinates без kubelet pin либо credentials.
type boundLogsAccess struct {
	reader                  targetrepo.IdentityMaterialReader
	identity                value.KubernetesIdentityLocator
	subject, metadataDigest string
	protected               []string
	pod                     PodStreamTarget
	input                   managedkubernetes.OperationInput
	address                 ResourceAddress
}

// VerifyKubernetesStreamAccess repeats selected-identity API authentication and exact pods/log permission for each output credit.
// VerifyKubernetesStreamAccess повторяет selected-identity API authentication и exact pods/log permission для каждого output credit.
func (access *boundLogsAccess) VerifyKubernetesStreamAccess(ctx context.Context, current value.KubernetesOperationSnapshot) (time.Time, error) {
	_, deadline, err := access.authorizedCredential(ctx, current)
	return deadline, err
}

// authorizedCredential refuses changed identity, target, Pod incarnation or current permission before one admitted API request.
// Mutable Pod resource versions are reread rather than pinned for the lifetime of a follow stream.
// authorizedCredential отклоняет changed identity, target, Pod incarnation либо current permission до одного admitted API request.
// Mutable Pod resource versions перечитываются, а не закрепляются на весь lifetime follow stream.
func (access *boundLogsAccess) authorizedCredential(ctx context.Context, current value.KubernetesOperationSnapshot) (Credential, time.Time, error) {
	denied := targetrepo.ErrOperationUnavailable
	if access == nil || ctx == nil || ctx.Err() != nil || current.NodeTransport != nil || current.Target.MetadataDigest() != access.metadataDigest || current.Identity != access.identity || current.IdentityRef != access.identity.IdentityRef || current.SubjectDigest != access.subject || !slices.Equal(current.ProtectedNamespaces, access.protected) || !current.ExpiresAt.After(time.Now()) {
		return Credential{}, time.Time{}, denied
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	credential, err := LoadCredential(check, access.reader, access.identity, current.Target, now)
	if err != nil {
		return Credential{}, time.Time{}, denied
	}
	deadline, err := credentialOutputDeadline(credential, now, current.ExpiresAt)
	if err != nil {
		return Credential{}, time.Time{}, denied
	}
	credit, finish := context.WithDeadline(check, deadline)
	defer finish()
	_, _, _, err = credential.logsRequest(credit, access.input, access.address, access.pod.Container)
	requirements := []AccessRequirement{{Version: "v1", Resource: "pods", Subresource: "log", Namespace: access.address.Namespace, Name: access.address.Name, Verb: "get", Namespaced: true}}
	if err != nil || credential.VerifyClusterIdentity(credit) != nil || credential.Authorize(credit, access.subject, requirements) != nil {
		return Credential{}, time.Time{}, denied
	}
	var object map[string]any
	if credential.readResourcePolicyObject(credit, access.subject, access.address, &object) != nil || credential.ValidateQualifiedResourcePolicy(credit, "kubernetes.logs", access.address, "Pod", "", access.subject, object, access.protected) != nil || !matchesCurrentStreamPod(object, access.pod, "kubernetes.logs") || credit.Err() != nil || !deadline.After(time.Now()) {
		return Credential{}, time.Time{}, denied
	}
	return credential, deadline, nil
}

// String hides identity locators and private log request metadata.
// String скрывает identity locators и private log request metadata.
func (*boundLogsAccess) String() string { return "[managed Kubernetes log authorization]" }

// GoString preserves the diagnostic redaction boundary.
// GoString сохраняет границу diagnostic redaction.
func (access *boundLogsAccess) GoString() string { return access.String() }
