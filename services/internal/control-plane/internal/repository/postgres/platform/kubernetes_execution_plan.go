package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesPlanDetail reads the immutable plan bound to the exact original claim.
// queryKubernetesPlanDetail читает immutable plan, связанный с exact original claim.
//
//go:embed sql/kubernetes_execution__plan_detail.sql
var queryKubernetesPlanDetail string

// queryKubernetesChildrenRead reads immutable proposals and separate current attempt states.
// queryKubernetesChildrenRead читает immutable proposals и отдельные current attempt states.
//
//go:embed sql/kubernetes_execution__children_read.sql
var queryKubernetesChildrenRead string

// queryKubernetesPlanInsert allocates one original hard deadline from a single database clock.
// queryKubernetesPlanInsert назначает original hard deadline из одного database clock.
//
//go:embed sql/kubernetes_execution__plan_insert.sql
var queryKubernetesPlanInsert string

// queryKubernetesChildInsert binds the child to an exact current grant without accepting actor refs.
// queryKubernetesChildInsert связывает child с exact current grant без actor refs от caller.
//
//go:embed sql/kubernetes_execution__child_insert.sql
var queryKubernetesChildInsert string

// queryKubernetesChildCurrent reuses the canonical grant predicate before committing a new child.
// queryKubernetesChildCurrent повторно использует canonical grant predicate до commit нового child.
//
//go:embed sql/kubernetes_execution__child_current.sql
var queryKubernetesChildCurrent string

// kubernetesPlanDigest binds immutable plan lineage and children independently of mutable attempts.
// Its ordering is owner-assigned; a receipt or lease renewal never changes these bytes.
// kubernetesPlanDigest связывает immutable plan lineage/children отдельно от mutable attempts.
// Порядок назначает owner; receipt либо lease renewal не меняют эти bytes.
func kubernetesPlanDigest(bindingDigest, snapshotDigest, proposalDigest string, children []entity.ManagedKubernetesChildEffect) (string, error) {
	// immutableChild excludes changing attempt state from the permanent plan hash.
	// immutableChild исключает changing attempt state из permanent plan hash.
	type immutableChild struct {
		Ref, Operation, InputDigest string
		Resource                    value.ManagedKubernetesResourceAddress
		NodeTransport               *value.ManagedKubernetesNodeTransport
		Observation                 *managedkubernetes.ChildCandidate
	}
	entries := make([]immutableChild, 0, len(children))
	for _, child := range children {
		entries = append(entries, immutableChild{child.Ref, child.Operation, child.InputDigest, child.Resource, child.NodeTransport, child.Observation})
	}
	raw, err := json.Marshal(struct {
		Domain, BindingDigest, SnapshotDigest, ProposalDigest string
		Children                                              []immutableChild
	}{"kodex.managed-kubernetes.plan/v1", bindingDigest, snapshotDigest, proposalDigest, entries})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// kubernetesDirectChild validates one observed resource against the original owner input/scope.
// Discovery evidence cannot retarget a request. The original proposal may omit NodeUID; the owner
// enriches a separate final observation from the exact current operator-attested node pin, never
// claiming that this metadata is a selected-identity GET Node observation.
// kubernetesDirectChild проверяет observed resource по original owner input/scope.
// Discovery evidence не меняет target. Если original proposal не содержит NodeUID, owner
// дополняет отдельное final observation из exact current operator-attested node pin, не выдавая
// эти metadata за selected-identity GET Node observation.
func kubernetesDirectChild(owner kubernetesOperationOwner, candidate managedkubernetes.ChildCandidate) (entity.ManagedKubernetesChildEffect, error) {
	result := entity.ManagedKubernetesChildEffect{Operation: owner.candidate.operation, NormalizedInputJSON: append([]byte(nil), owner.normalized...), State: "READY", Resource: candidate.Resource, Observation: &candidate}
	descriptor, known := integrationpackage.KubernetesOperationByKey(result.Operation)
	if !known || (descriptor.Execution != integrationpackage.KubernetesResource && descriptor.Execution != integrationpackage.KubernetesStream) || candidate.ValidateProposal() != nil {
		return result, errs.ErrInvalid
	}
	if result.Operation == "kubernetes.debug" {
		return result, errs.ErrUnavailable
	}
	input, address := owner.input, candidate.Resource
	var grantScope map[string]string
	if json.Unmarshal(owner.candidate.resourceScope, &grantScope) != nil {
		return result, errs.ErrUnavailable
	}
	if address.Group != input.Group || address.Version != input.Version || address.Resource != input.Resource || address.Namespace != input.Namespace || address.Subresource != input.Subresource || address.Namespaced != (grantScope["namespace"] != "@cluster") || address.AllNamespaces != (grantScope["namespace"] == "@all-namespaces") {
		return result, errs.ErrForbidden
	}
	if input.UID != "" && input.UID != address.UID || input.ResourceVersion != "" && input.ResourceVersion != address.ResourceVersion {
		return result, errs.ErrForbidden
	}
	if descriptor.Verb == "create" {
		if candidate.ObservationState != managedkubernetes.ObservationCollection || address.Name != "" {
			return result, errs.ErrForbidden
		}
	} else if address.Name != input.Name {
		return result, errs.ErrForbidden
	}
	if candidate.ObservationState == managedkubernetes.ObservationUnversionedRead && owner.candidate.risk != "READ" {
		return result, errs.ErrForbidden
	}
	if owner.candidate.risk != "READ" && candidate.ObservationState != managedkubernetes.ObservationPresent && descriptor.Verb != "create" {
		return result, errs.ErrForbidden
	}
	if managedkubernetes.ValidateInvocationApprovalPolicy(input, owner.candidate.risk, owner.candidate.approvalPolicy) != nil {
		return result, errs.ErrForbidden
	}
	if candidate.PodWitness != nil {
		var options struct {
			Container string `json:"container"`
		}
		if len(input.Options) != 0 && json.Unmarshal(input.Options, &options) != nil {
			return result, errs.ErrUnavailable
		}
		if options.Container != "" && options.Container != candidate.PodWitness.Container {
			return result, errs.ErrForbidden
		}
		if descriptor.Execution != integrationpackage.KubernetesStream {
			return result, errs.ErrForbidden
		}
		for _, entry := range owner.selected.source.registry.Nodes {
			pin := entry.Transport
			protocolMatches := pin.Protocol == managedkubernetes.WebSocketV5 || pin.Protocol == managedkubernetes.SPDYV4
			if result.Operation == "kubernetes.port-forward" {
				protocolMatches = pin.Protocol == managedkubernetes.PortForwardWebSocketV4 || pin.Protocol == managedkubernetes.PortForwardSPDYV1
			}
			if protocolMatches && !entry.Retired && !pin.NotBefore.After(time.Now().UTC()) && pin.TargetRef == owner.selected.target.Ref && pin.TargetRevision == owner.selected.target.Revision && pin.NodeName == candidate.PodWitness.NodeName && (candidate.PodWitness.NodeUID == "" || pin.NodeUID == candidate.PodWitness.NodeUID) && pin.ExpiresAt.After(time.Now().UTC()) {
				if result.NodeTransport != nil {
					return result, errs.ErrConflict
				}
				selected := pin
				result.NodeTransport = &selected
			}
		}
		if result.NodeTransport == nil {
			return result, errs.ErrForbidden
		}
		witness := *candidate.PodWitness
		witness.NodeUID = result.NodeTransport.NodeUID
		candidate.PodWitness = &witness
		result.Observation = &candidate
	}
	if descriptor.Execution == integrationpackage.KubernetesStream {
		privileged := result.Operation != "kubernetes.watch" && result.Operation != "kubernetes.watch.all-namespaces" && result.Operation != "kubernetes.logs"
		if privileged != (result.NodeTransport != nil) {
			return result, errs.ErrForbidden
		}
	}
	if result.Observation.Validate() != nil {
		return result, errs.ErrInvalid
	}
	digest := sha256.Sum256(result.NormalizedInputJSON)
	result.InputDigest = hex.EncodeToString(digest[:])
	return result, nil
}

// readKubernetesPlanChildren reads only stored immutable input and its current attempt state.
// It performs no target discovery or credential read, including accepted-result recovery.
// readKubernetesPlanChildren читает только stored immutable input и current attempt state.
// Не выполняет target discovery либо credential read, включая accepted-result recovery.
func readKubernetesPlanChildren(ctx context.Context, tx pgx.Tx, organizationID, planID string, count int, nodes []value.KubernetesOperatorNode) ([]entity.ManagedKubernetesChildEffect, error) {
	if count < 0 || count > 256 {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryKubernetesChildrenRead, pgx.StrictNamedArgs{"organization_id": organizationID, "plan_id": planID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.ManagedKubernetesChildEffect, 0, count)
	for rows.Next() {
		var child entity.ManagedKubernetesChildEffect
		var observationRaw []byte
		var inputJSON, nodeRef string
		var nodeGeneration int64
		if rows.Scan(&child.Ref, &child.Operation, &child.InputDigest, &inputJSON, &observationRaw, &nodeRef, &nodeGeneration, &child.State) != nil {
			return nil, errs.ErrUnavailable
		}
		if integrationpackage.KubernetesMetadataRead(child.Operation) {
			var empty struct{}
			if internalrpcauth.DecodeStrictJSON(observationRaw, &empty) != nil || string(observationRaw) != "{}" || nodeRef != "" || nodeGeneration != 0 {
				return nil, errs.ErrUnavailable
			}
		} else {
			var observation managedkubernetes.ChildCandidate
			if internalrpcauth.DecodeStrictJSON(observationRaw, &observation) != nil || observation.Validate() != nil {
				return nil, errs.ErrUnavailable
			}
			child.Resource = observation.Resource
			child.Observation = &observation
		}
		child.NormalizedInputJSON = []byte(inputJSON)
		_, canonical, decodeErr := managedkubernetes.DecodeOperationInput(child.Operation, child.NormalizedInputJSON)
		digest := sha256.Sum256(child.NormalizedInputJSON)
		if decodeErr != nil || string(canonical) != inputJSON || hex.EncodeToString(digest[:]) != child.InputDigest || !managedkubernetes.ValidLocator(child.Ref) {
			return nil, errs.ErrUnavailable
		}
		if nodeRef != "" || nodeGeneration != 0 {
			for _, entry := range nodes {
				if entry.Transport.Ref == nodeRef && entry.Transport.Generation == nodeGeneration {
					pin := entry.Transport
					child.NodeTransport = &pin
				}
			}
			if child.NodeTransport == nil {
				return nil, errs.ErrUnavailable
			}
		}
		result = append(result, child)
		if len(result) > count {
			return nil, errs.ErrUnavailable
		}
	}
	if rows.Err() != nil || len(result) != count {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// PrepareManagedKubernetesChildren stores an exact direct child before any effect admission.
// Existing plans return original immutable rows without re-observation or new credit. Composite
// expansion has a separate finite recipe; this path never turns arbitrary proposals into fan-out.
// PrepareManagedKubernetesChildren сохраняет exact direct child до effect admission.
// Existing plans возвращают original immutable rows без re-observation либо нового credit.
// Composite expansion требует finite recipe; arbitrary proposals не превращаются в fan-out.
func (repository *Repository) PrepareManagedKubernetesChildren(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesPrepareChildren) (result entity.ManagedKubernetesPlan, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.children.prepare"); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return result, err
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	if snapshot != input.SnapshotDigest {
		return result, errs.ErrForbidden
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": snapshot}
	var planID, proposalDigest string
	var count int
	var deadline time.Time
	err = tx.QueryRow(ctx, queryKubernetesPlanDetail, args).Scan(&planID, &result.Ref, &result.Digest, &proposalDigest, &count, &deadline)
	if err == nil {
		if len(input.Candidates) != 0 && proposalDigest != input.ProposalDigest {
			return result, errs.ErrConflict
		}
		result.Children, err = readKubernetesPlanChildren(ctx, tx, current.organizationID, planID, count, owner.selected.source.registry.Nodes)
		if err != nil {
			return entity.ManagedKubernetesPlan{}, err
		}
		expected, err := kubernetesPlanDigest(bindingDigest, snapshot, proposalDigest, result.Children)
		if err != nil || expected != result.Digest {
			return entity.ManagedKubernetesPlan{}, errs.ErrUnavailable
		}
		result.Revision = 1
		if tx.Commit(ctx) != nil {
			return entity.ManagedKubernetesPlan{}, errs.ErrUnavailable
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	metadata := integrationpackage.KubernetesMetadataRead(owner.candidate.operation)
	if (!metadata && len(input.Candidates) != 1) || (metadata && len(input.Candidates) != 0) {
		return result, errs.ErrInvalid
	}
	expected, err := managedkubernetes.ChildProposalDigest(snapshot, input.Candidates)
	if err != nil || expected != input.ProposalDigest {
		return result, errs.ErrInvalid
	}
	var child entity.ManagedKubernetesChildEffect
	if metadata {
		child, err = kubernetesMetadataChild(owner)
	} else {
		child, err = kubernetesDirectChild(owner, input.Candidates[0])
	}
	if err != nil {
		return result, err
	}
	result.Ref, err = newRef("kpl")
	if err != nil {
		return result, err
	}
	child.Ref, err = newRef("kch")
	if err != nil {
		return result, err
	}
	result.Children = []entity.ManagedKubernetesChildEffect{child}
	result.Revision = 1
	result.Digest, err = kubernetesPlanDigest(bindingDigest, snapshot, expected, result.Children)
	if err != nil {
		return result, err
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, owner.candidate.connectionRef, owner.candidate.definitionKey, owner.candidate.definitionVersion, owner.candidate.definitionDigest)
	if err != nil {
		return result, err
	}
	capability, known := definition.Capability(owner.candidate.capabilityKey)
	if !known || capability.Execution.TimeoutSeconds < 1 || capability.Execution.TimeoutSeconds > 600 {
		return result, errs.ErrUnavailable
	}
	runtime, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, owner.candidate.runID)
	if err != nil {
		return result, err
	}
	if expired {
		return result, errs.ErrForbidden
	}
	var runtimeDeadline *time.Time
	if runtime != nil {
		runtimeDeadline = &runtime.EffectiveDeadlineAt
	}
	if child.NodeTransport != nil && (runtimeDeadline == nil || child.NodeTransport.ExpiresAt.Before(*runtimeDeadline)) {
		deadline := child.NodeTransport.ExpiresAt
		runtimeDeadline = &deadline
	}
	insert := pgx.StrictNamedArgs{"plan_ref": result.Ref, "organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": snapshot, "proposal_digest": expected, "plan_digest": result.Digest, "child_count": 1, "timeout_seconds": kubernetesOperationTimeout(capability.Execution.TimeoutSeconds, owner.input), "runtime_deadline": runtimeDeadline}
	if err := tx.QueryRow(ctx, queryKubernetesPlanInsert, insert).Scan(&planID, &deadline); err != nil {
		return entity.ManagedKubernetesPlan{}, mapWriteError(err)
	}
	raw, err := json.Marshal(child.Observation)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if metadata {
		raw = []byte("{}")
	}
	nodeRef := ""
	var nodeGeneration int64
	if child.NodeTransport != nil {
		nodeRef = child.NodeTransport.Ref
		nodeGeneration = child.NodeTransport.Generation
	}
	childArgs := pgx.StrictNamedArgs{"child_ref": child.Ref, "organization_id": current.organizationID, "plan_id": planID, "ordinal": 0, "operation": child.Operation, "risk": owner.candidate.risk, "approval_policy": owner.candidate.approvalPolicy, "capability_key": owner.candidate.capabilityKey, "grant_version": owner.candidate.grantVersion, "grant_scope_digest": owner.candidate.resourceScopeDigest, "input_json": string(child.NormalizedInputJSON), "input_digest": child.InputDigest, "observation_json": string(raw), "node_ref": nodeRef, "node_generation": nodeGeneration, "grant_ref": owner.candidate.grantRef}
	var childID string
	if err := tx.QueryRow(ctx, queryKubernetesChildInsert, childArgs).Scan(&childID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ManagedKubernetesPlan{}, errs.ErrForbidden
		}
		return entity.ManagedKubernetesPlan{}, mapWriteError(err)
	}
	var valid bool
	if err := tx.QueryRow(ctx, queryKubernetesChildCurrent, pgx.StrictNamedArgs{"child_id": childID, "invocation_id": owner.candidate.id}).Scan(&valid); err != nil {
		return entity.ManagedKubernetesPlan{}, errs.ErrUnavailable
	}
	if !valid {
		return entity.ManagedKubernetesPlan{}, errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesPlan{}, errs.ErrUnavailable
	}
	return result, nil
}

// kubernetesOperationTimeout narrows the package ceiling to the already validated input deadline.
// It extracts metadata from the shared closed schema output and never accepts a new authority field.
// kubernetesOperationTimeout сужает package ceiling по уже validated input deadline.
// Извлекает metadata из shared closed schema output без нового authority field.
func kubernetesOperationTimeout(packageSeconds int, input managedkubernetes.OperationInput) int {
	var options struct {
		TimeoutSeconds int `json:"timeoutSeconds"`
	}
	if len(input.Options) != 0 && json.Unmarshal(input.Options, &options) != nil {
		return 0
	}
	if options.TimeoutSeconds > 0 && options.TimeoutSeconds < packageSeconds {
		return options.TimeoutSeconds
	}
	return packageSeconds
}
