package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

const (
	// resourceOperationInvalid refuses a changed snapshot or invalid local phase without provider data.
	// resourceOperationInvalid отклоняет changed snapshot либо invalid local phase без provider data.
	resourceOperationInvalid = "managed Kubernetes resource operation is invalid"
	// resourceMaterialRequired distinguishes missing protected material from a runnable Secret effect.
	// resourceMaterialRequired отличает missing protected material от runnable Secret effect.
	resourceMaterialRequired = "managed Kubernetes protected resource material is required"
)

// PreparedResourceOperation retains one validated immutable proposal and private provider state.
// Preparation performs discovery/reviews/current-object reads only. Execute still requires a
// fresh upper-owner Begin and deadline; this type cannot issue or renew that authority itself.
// Close cancels and joins an in-flight request before dropping private state. No raw body or
// credential is returned by Observation, formatting or an error.
// PreparedResourceOperation удерживает один validated immutable proposal и private provider state.
// Preparation выполняет только discovery/reviews/current-object reads. Execute требует свежий
// upper-owner Begin/deadline; сам тип не выдаёт и не продлевает authority. Close отменяет и
// присоединяет in-flight request до удаления private state. Observation, formatting и error
// не выдают raw body либо credential.
type PreparedResourceOperation struct {
	mutex         sync.Mutex
	closed        bool
	consumed      bool
	materialBound bool
	materialTried bool
	ready         bool
	done          chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	credential    Credential
	operation     integrationpackage.KubernetesOperation
	input         managedkubernetes.OperationInput
	inputDigest   string
	address       ResourceAddress
	subjectDigest string
	protected     []string
	schema        *registeredResourceSchema
	observation   managedkubernetes.ResourceObservation
	current       map[string]any
	parent        map[string]any
	proposed      map[string]any
	materialRef   string
	material      []byte
	body          []byte
	method        string
	contentType   string
	query         url.Values
}

// String hides every private preparation field, including object and credential material.
// String скрывает каждое private preparation field, включая object/credential material.
func (*PreparedResourceOperation) String() string {
	return "[managed Kubernetes prepared resource operation]"
}

// GoString applies the same redaction to detailed Go formatting.
// GoString сохраняет ту же redaction при detailed Go formatting.
func (prepared *PreparedResourceOperation) GoString() string { return prepared.String() }

// PrepareResourceOperation validates the registered shared input and selected-identity discovery
// before producing one owner proposal. CREATE uses an explicit collection proposal and one later
// POST, so it neither invents an ABSENT witness nor adds mandatory object/namespace GET permission.
// Existing mutations/APPLY read and pin their current object; every actual Execute repeats fresh
// subject/RBAC/cluster checks after the upper owner has admitted the complete prepared proposal.
// PrepareResourceOperation проверяет registered shared input и selected-identity discovery
// до создания одного owner proposal. CREATE использует explicit collection proposal и один
// поздний POST: не выдумывает ABSENT witness и не требует лишний object/namespace GET.
// Existing mutations/APPLY читают и закрепляют current object; каждый actual Execute повторяет
// fresh subject/RBAC/cluster checks после upper-owner admission полного prepared proposal.
func (credential Credential) PrepareResourceOperation(ctx context.Context, operation string, input managedkubernetes.OperationInput, address ResourceAddress, subjectDigest string, protectedNamespaces []string) (*PreparedResourceOperation, error) {
	registered, known := integrationpackage.KubernetesOperationByKey(operation)
	raw, err := managedkubernetes.EncodeOperationInput(operation, input)
	if err != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	defer clear(raw)
	owned, canonical, err := managedkubernetes.DecodeOperationInput(operation, raw)
	defer clear(canonical)
	metadata := integrationpackage.KubernetesMetadataRead(operation)
	if ctx == nil || err != nil || !known || validateTarget(credential.target) != nil ||
		(!metadata && (registered.Execution != integrationpackage.KubernetesResource || address.Validate() != nil || address.AllNamespaces != (operation == "kubernetes.list.all-namespaces") || resourceNamespaceProtected(address, protectedNamespaces) || !resourceInputAddressMatches(owned, address))) ||
		(metadata && (address != (ResourceAddress{}) || owned.Resource != "")) {
		return nil, errors.New(resourceOperationInvalid)
	}
	base, cancel := context.WithCancel(ctx)
	inputSum := sha256.Sum256(canonical)
	prepared := &PreparedResourceOperation{ctx: base, cancel: cancel, credential: credential,
		operation: registered, input: owned, inputDigest: hex.EncodeToString(inputSum[:]), address: address, subjectDigest: subjectDigest,
		protected: slices.Clone(protectedNamespaces)}
	accepted := false
	defer func() {
		if !accepted {
			prepared.Close()
		}
	}()
	probe, stopProbe := context.WithTimeout(base, 30*time.Second)
	defer stopProbe()
	if credential.VerifyClusterIdentity(probe) != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	if metadata {
		if prepared.authorizeMetadata(probe) != nil || probe.Err() != nil {
			return nil, errors.New(resourceOperationInvalid)
		}
		prepared.ready, accepted = true, true
		return prepared, nil
	}
	prepared.schema, err = credential.discoverResource(probe, subjectDigest, address, registered)
	if err != nil {
		return nil, err
	}
	if err = prepared.prepareObservation(probe); err != nil {
		return nil, err
	}
	options, err := resourceOperationOptions(owned)
	if err != nil {
		return nil, err
	}
	prepared.materialRef, _ = options["secretMaterialRef"].(string)
	if prepared.materialRef == "" {
		if err = prepared.buildResourceRequest(nil); err != nil {
			return nil, err
		}
		prepared.ready = true
	}
	if probe.Err() != nil {
		return nil, errors.New(resourceOperationInvalid)
	}
	accepted = true
	return prepared, nil
}

// Observation returns a value-only proposal after all required private material and validation
// are complete. A closed, poisoned or material-pending preparation cannot masquerade as ready.
// Observation возвращает value-only proposal после полного private material/validation.
// Closed, poisoned либо material-pending preparation не может выглядеть ready.
func (prepared *PreparedResourceOperation) Observation() managedkubernetes.ResourceObservation {
	if prepared == nil {
		return managedkubernetes.ResourceObservation{}
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	if prepared.closed || !prepared.ready {
		return managedkubernetes.ResourceObservation{}
	}
	return prepared.observation
}

// Binding atomically captures the exact native proposal before admission without exposing private input.
// Binding атомарно фиксирует exact native proposal перед admission без раскрытия private input.
func (prepared *PreparedResourceOperation) Binding() (targetrepo.ResourcePreparationBinding, bool) {
	if prepared == nil {
		return targetrepo.ResourcePreparationBinding{}, false
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	if prepared.closed || prepared.consumed || !prepared.ready {
		return targetrepo.ResourcePreparationBinding{}, false
	}
	return targetrepo.ResourcePreparationBinding{Operation: prepared.operation.Key, InputDigest: prepared.inputDigest,
		TargetDigest: prepared.credential.target.MetadataDigest(), SubjectDigest: prepared.subjectDigest, Observation: prepared.observation}, true
}

// RegisteredKind exposes only safe discovery metadata, not readiness or resource permission.
// RegisteredKind выдаёт только safe discovery metadata, не readiness либо resource permission.
func (prepared *PreparedResourceOperation) RegisteredKind() string {
	if prepared == nil {
		return ""
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	if prepared.closed || prepared.schema == nil {
		return ""
	}
	return prepared.schema.kind.Kind
}

// BindSecretMaterial immediately takes exclusive ownership even on failure. Only one binding
// attempt is permitted, with the exact public locator and independently owner-resolved digest.
// No material target selector can change the prepared route, identity or current-object CAS.
// BindSecretMaterial сразу принимает exclusive ownership даже при отказе. Допустима одна binding
// attempt с exact public locator и независимо owner-resolved digest. Material target selector
// не меняет prepared route, identity либо current-object CAS.
func (prepared *PreparedResourceOperation) BindSecretMaterial(ref string, owned []byte, expectedSHA256 string) error {
	if prepared == nil {
		clear(owned)
		return errors.New(resourceOperationInvalid)
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	retained := false
	defer func() {
		if !retained {
			clear(owned)
		}
	}()
	if prepared.closed || prepared.consumed || prepared.materialTried || prepared.materialRef == "" {
		return errors.New(resourceOperationInvalid)
	}
	prepared.materialTried = true
	if ref != prepared.materialRef {
		return errors.New(resourceOperationInvalid)
	}
	actual := sha256.Sum256(owned)
	if len(owned) == 0 || len(owned) > resourceResponseBytes || !managedkubernetes.ValidDigest(expectedSHA256) ||
		hex.EncodeToString(actual[:]) != expectedSHA256 || prepared.ctx.Err() != nil {
		return errors.New(resourceOperationInvalid)
	}
	if err := prepared.buildResourceRequest(owned); err != nil {
		return err
	}
	prepared.material, prepared.materialBound, prepared.ready = owned, true, true
	retained = true
	return nil
}

// Execute consumes this preparation once after fresh upper-owner Begin. The deadline is never
// extended, the mutation has one wire attempt, and an ambiguous response cannot become success.
// Fresh owner completion/result credit still belongs to the caller before publishing SafeJSON.
// Execute расходует preparation один раз после fresh upper-owner Begin. Deadline не расширяется,
// mutation имеет одну wire attempt; ambiguous response не становится success. Fresh owner
// completion/result credit остаётся у caller до выдачи SafeJSON.
func (prepared *PreparedResourceOperation) Execute(ctx context.Context, authorizedUntil time.Time) (value.ResourceOperationResult, error) {
	if prepared == nil {
		return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonScopeDenied}, errors.New(resourceOperationInvalid)
	}
	prepared.mutex.Lock()
	if prepared.closed || prepared.consumed || !prepared.ready || ctx == nil || prepared.ctx.Err() != nil ||
		!time.Now().Before(authorizedUntil) || authorizedUntil.After(time.Now().Add(30*time.Second)) {
		pending := !prepared.ready && prepared.materialRef != ""
		prepared.mutex.Unlock()
		if pending {
			return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonResourceUnavailable}, errors.New(resourceMaterialRequired)
		}
		return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonScopeDenied}, errors.New(resourceOperationInvalid)
	}
	prepared.consumed = true
	prepared.done = make(chan struct{})
	prepared.mutex.Unlock()
	defer close(prepared.done)
	bounded, cancel := context.WithDeadline(ctx, authorizedUntil)
	defer cancel()
	stop := context.AfterFunc(prepared.ctx, cancel)
	defer stop()
	if integrationpackage.KubernetesMetadataRead(prepared.operation.Key) {
		return prepared.executeMetadataRequest(bounded)
	}
	if prepared.credential.VerifyClusterIdentity(bounded) != nil || prepared.authorizeResource(bounded) != nil {
		return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonTargetUnavailable}, errors.New(resourceOperationInvalid)
	}
	if prepared.operation.Risk != "READ" {
		address, kind, current := prepared.address, prepared.schema.kind.Kind, prepared.current
		if prepared.parent != nil {
			address.Subresource = ""
			current = prepared.parent
			kind, _ = current["kind"].(string)
		}
		for _, object := range []map[string]any{current, prepared.proposed} {
			if object != nil {
				if err := prepared.credential.ValidateQualifiedResourcePolicy(bounded, prepared.operation.Key, address, kind, prepared.schema.digest, prepared.subjectDigest, object, prepared.protected); err != nil {
					return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: ResourcePolicyReason(err)}, errors.New(resourcePolicyForbidden)
				}
			}
		}
	}
	return prepared.executeResourceRequest(bounded, authorizedUntil)
}

// validateResourcePolicy evaluates a prepared object's actual operator qualification without
// turning immutable input or schema labels into authority. Each evidence read remains bounded.
// validateResourcePolicy проверяет actual operator qualification prepared object, не превращая
// immutable input/schema labels в authority. Каждый evidence read ограничен по времени.
func (prepared *PreparedResourceOperation) validateResourcePolicy(address ResourceAddress, kind string, object map[string]any) error {
	return prepared.credential.ValidateQualifiedResourcePolicy(prepared.ctx, prepared.operation.Key, address, kind,
		prepared.schema.digest, prepared.subjectDigest, object, prepared.protected)
}

// Close cancels and joins a consumed operation before clearing owned buffers and private maps.
// Immutable string copies may survive until garbage collection; no physical-erasure claim is made.
// Close отменяет и присоединяет consumed operation до очистки owned buffers/private maps.
// Immutable string copies могут жить до garbage collection; physical erasure не обещается.
func (prepared *PreparedResourceOperation) Close() {
	if prepared == nil {
		return
	}
	prepared.mutex.Lock()
	prepared.closed = true
	if prepared.cancel != nil {
		prepared.cancel()
	}
	done := prepared.done
	prepared.mutex.Unlock()
	if done != nil {
		<-done
	}
	prepared.mutex.Lock()
	defer prepared.mutex.Unlock()
	clear(prepared.material)
	clear(prepared.body)
	clear(prepared.input.Body)
	clear(prepared.input.Options)
	prepared.material, prepared.body, prepared.current, prepared.parent, prepared.proposed = nil, nil, nil, nil, nil
	prepared.input, prepared.credential = managedkubernetes.OperationInput{}, Credential{}
	prepared.schema, prepared.query = nil, nil
}

// resourceInputAddressMatches prevents a private binding from silently changing immutable input
// coordinates. Discovery separately verifies the Namespaced/AllNamespaces booleans.
// resourceInputAddressMatches не позволяет private binding молча изменить immutable input
// coordinates. Namespaced/AllNamespaces booleans отдельно проверяет discovery.
func resourceInputAddressMatches(input managedkubernetes.OperationInput, address ResourceAddress) bool {
	return input.Group == address.Group && input.Version == address.Version && input.Resource == address.Resource &&
		input.Namespace == address.Namespace && input.Name == address.Name && input.Subresource == address.Subresource &&
		(input.UID == "" || input.UID == address.UID) && (input.ResourceVersion == "" || input.ResourceVersion == address.ResourceVersion)
}

// resourceOperationOptions decodes only already validated shared option keys and types.
// resourceOperationOptions разбирает только ранее проверенные shared option keys/types.
func resourceOperationOptions(input managedkubernetes.OperationInput) (map[string]any, error) {
	if len(input.Options) == 0 {
		return map[string]any{}, nil
	}
	return decodeResourceJSON(input.Options, managedkubernetes.MaximumOperationInputBytes, 16)
}
