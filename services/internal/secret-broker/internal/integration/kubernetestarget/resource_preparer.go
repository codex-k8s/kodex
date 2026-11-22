package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// ResourcePreparer connects purpose-specific identity storage to the native resource transport.
// Credentials stay in the broker and never enter the operation result or a generic shell.
// ResourcePreparer соединяет purpose-specific identity storage с native resource transport.
// Credentials остаются в broker и не входят в operation result либо generic shell.
type ResourcePreparer struct {
	reader    targetrepo.IdentityMaterialReader
	namespace string
}

// NewResourcePreparer requires an explicit private namespace and exact material reader.
// NewResourcePreparer требует explicit private namespace и exact material reader.
func NewResourcePreparer(reader targetrepo.IdentityMaterialReader, namespace string) (*ResourcePreparer, error) {
	address := managedkubernetes.ResourceAddress{Version: "v1", Resource: "secrets", Namespaced: true, Namespace: namespace}
	if reader == nil || address.Validate() != nil || namespace == "kodex-system" {
		return nil, targetrepo.ErrOperationUnavailable
	}
	return &ResourcePreparer{reader: reader, namespace: namespace}, nil
}

// PrepareKubernetesResource loads the pinned identity once and preserves caller lifetime for the prepared request.
// The short storage context must not cancel a successfully prepared operation before its owner Begin.
// PrepareKubernetesResource загружает pinned identity один раз и сохраняет caller lifetime для prepared request.
// Короткий storage context не отменяет успешную подготовку до owner Begin.
func (factory *ResourcePreparer) PrepareKubernetesResource(ctx context.Context, snapshot value.KubernetesOperationSnapshot) (targetrepo.ResourcePreparation, error) {
	metadata := integrationpackage.KubernetesMetadataRead(snapshot.Operation)
	if factory == nil || ctx == nil || ctx.Err() != nil || snapshot.Identity.Validate(factory.namespace) != nil || (!metadata && snapshot.Resource == nil) || (metadata && (snapshot.Resource != nil || snapshot.Discovery != nil)) || !snapshot.ExpiresAt.After(time.Now()) || !managedkubernetes.ValidDigest(snapshot.SubjectDigest) {
		return nil, targetrepo.ErrOperationUnavailable
	}
	read, cancel := context.WithTimeout(ctx, 5*time.Second)
	credential, err := LoadCredential(read, factory.reader, snapshot.Identity, snapshot.Target, time.Now())
	readErr := read.Err()
	cancel()
	if err != nil || readErr != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	var address ResourceAddress
	if snapshot.Resource != nil {
		address = *snapshot.Resource
	}
	prepared, err := credential.PrepareResourceOperation(ctx, snapshot.Operation, snapshot.Input, address, snapshot.SubjectDigest, snapshot.ProtectedNamespaces)
	if err != nil {
		return nil, targetrepo.ErrOperationUnavailable
	}
	if _, ready := prepared.Binding(); !ready {
		// Protected Secret material requires its own exact owner-bound path, never an inline body fallback.
		// Protected Secret material требует отдельного exact owner-bound path без inline body fallback.
		prepared.Close()
		return nil, targetrepo.ErrOperationUnavailable
	}
	return prepared, nil
}

// AuthorizeKubernetesResourceResult checks current authentication and exact READ RBAC without resource GET or discovery.
// It uses only the owner-selected purpose credential; revocation never falls back to broker service credentials.
// AuthorizeKubernetesResourceResult проверяет current authentication и exact READ RBAC без resource GET/discovery.
// Он использует только owner-selected purpose credential без fallback на broker service credentials при revoke.
func (factory *ResourcePreparer) AuthorizeKubernetesResourceResult(ctx context.Context, snapshot value.KubernetesOperationSnapshot, child targetrepo.OperationChild) (time.Time, error) {
	operation, known := integrationpackage.KubernetesOperationByKey(child.Operation)
	metadata := integrationpackage.KubernetesMetadataRead(child.Operation)
	if factory == nil || ctx == nil || ctx.Err() != nil || !known || operation.Risk != integrationpackage.RiskRead || (!metadata && operation.Execution != integrationpackage.KubernetesResource) || snapshot.Identity.Validate(factory.namespace) != nil {
		return time.Time{}, targetrepo.ErrOperationUnavailable
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	credential, err := LoadCredential(check, factory.reader, snapshot.Identity, snapshot.Target, time.Now())
	if err != nil || credential.VerifyClusterIdentity(check) != nil {
		return time.Time{}, targetrepo.ErrOperationUnavailable
	}
	if metadata {
		if child.Resource != (ResourceAddress{}) || child.Observation != nil || child.NodeTransport != nil || snapshot.Resource != nil || snapshot.Discovery != nil {
			return time.Time{}, targetrepo.ErrOperationUnavailable
		}
		prepared := PreparedResourceOperation{credential: credential, operation: operation, subjectDigest: snapshot.SubjectDigest}
		err = prepared.authorizeMetadata(check)
	} else {
		var requirements []AccessRequirement
		requirements, err = OperationAccessRequirements(child.Operation, child.Resource)
		if err == nil {
			err = credential.Authorize(check, snapshot.SubjectDigest, requirements)
		}
	}
	if err != nil || check.Err() != nil {
		return time.Time{}, targetrepo.ErrOperationUnavailable
	}
	return credentialOutputDeadline(credential, time.Now(), time.Now().Add(5*time.Second))
}

// The assertion keeps the production factory aligned with its credential-free domain port.
// Проверка сохраняет соответствие production factory и credential-free domain port.
var _ targetrepo.ResourcePreparer = (*ResourcePreparer)(nil)
