package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesGateConsumerOrigin identifies the specialized branch without trusting a payload kind.
// queryKubernetesGateConsumerOrigin определяет специализированную ветку без payload kind.
//
//go:embed sql/kubernetes_child_gate__origin.sql
var queryKubernetesGateConsumerOrigin string

// queryKubernetesGateConsumerDetail joins the original Gate/node/invocation/plan/child tenant graph.
// queryKubernetesGateConsumerDetail соединяет original Gate/node/invocation/plan/child tenant graph.
//
//go:embed sql/kubernetes_child_gate__detail.sql
var queryKubernetesGateConsumerDetail string

// kubernetesChildGateRecord retains private owner IDs only inside the repository transaction.
// Its public summary excludes the canonical executable body and all credential/lease material.
// kubernetesChildGateRecord хранит private owner IDs только внутри repository transaction.
// Public summary исключает canonical executable body и credential/lease material.
type kubernetesChildGateRecord struct {
	gateID, nodeID, nodeRef, rootID, projectID, projectRef, invocationID, invocationRef, planID, childID string
	version                                                                                              int64
	state, approvalPolicy, bindingDigest, snapshotDigest, resolutionKind                                 string
	allowed                                                                                              []string
	deadline, now                                                                                        time.Time
	summary                                                                                              entity.KubernetesOwnerGateChild
}

// kubernetesGatePreviewMode distinguishes explicit standalone reads from event reprojections.
// Actor-scoped event reads still carry metadata only; a viewing actor never changes that channel.
// kubernetesGatePreviewMode отличает explicit standalone reads от event reprojections.
// Actor-scoped event reads сохраняют лишь metadata независимо от прав читающего actor.
type kubernetesGatePreviewMode int

// These modes are private server choices; none is accepted from a request payload.
// Эти modes выбирает сервер; ни один не принимается из request payload.
const (
	kubernetesGatePreviewHidden kubernetesGatePreviewMode = iota
	kubernetesGatePreviewSummary
	kubernetesGatePreviewDetail
)

// kubernetesGatePreviewOptions carries server-only disclosure and response budget, never effect authority.
// kubernetesGatePreviewOptions несёт server-only disclosure и response budget, не effect authority.
type kubernetesGatePreviewOptions struct {
	disclose bool
	budget   int
}

// kubernetesChildGateOrigin detects a missing/orphaned specialized source before generic fallback.
// A known Kubernetes FK with a broken invocation is unavailable, never an ordinary parent Gate.
// kubernetesChildGateOrigin выявляет missing/orphaned specialized source до generic fallback.
// Kubernetes FK с повреждённым invocation — unavailable, не ordinary parent Gate.
func kubernetesChildGateOrigin(ctx context.Context, runner queryRunner, organizationID, gateRef string) (string, bool, error) {
	var specialized bool
	var invocationRef string
	err := runner.QueryRow(ctx, queryKubernetesGateConsumerOrigin, pgx.StrictNamedArgs{"organization_id": organizationID, "gate_ref": gateRef}).Scan(&specialized, &invocationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", true, errs.ErrUnavailable
	}
	if !specialized {
		return "", false, nil
	}
	if !managedkubernetes.ValidLocator(invocationRef) {
		return "", true, errs.ErrUnavailable
	}
	return invocationRef, true, nil
}

// readKubernetesChildGateRecord verifies immutable input and observation before any public projection.
// It reads no protected material and returns neither canonical input nor a broker binding.
// readKubernetesChildGateRecord проверяет immutable input/observation до public projection.
// Не читает protected material, не возвращает canonical input либо broker binding.
func readKubernetesChildGateRecord(ctx context.Context, runner queryRunner, organizationID, gateRef string, previewOptions ...kubernetesGatePreviewOptions) (kubernetesChildGateRecord, error) {
	var result kubernetesChildGateRecord
	var input, observed []byte
	defer func() { clear(input); clear(observed) }()
	child := &result.summary
	err := runner.QueryRow(ctx, queryKubernetesGateConsumerDetail, pgx.StrictNamedArgs{"organization_id": organizationID, "gate_ref": gateRef}).Scan(
		&result.gateID, &result.nodeID, &result.nodeRef, &result.rootID, &result.projectID, &result.projectRef, &result.version, &result.state, &result.allowed, &result.invocationID, &result.invocationRef,
		&result.planID, &child.PlanRef, &child.PlanDigest, &result.bindingDigest, &result.snapshotDigest, &result.deadline,
		&result.childID, &child.ChildRef, &child.Operation, &result.approvalPolicy, &input, &child.InputDigest, &observed, &child.TargetRef, &child.TargetRevision, &result.resolutionKind, &result.now)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if result.version < 1 || result.approvalPolicy != "HUMAN_EACH_EFFECT" || !managedkubernetes.ValidDigest(result.bindingDigest) || !managedkubernetes.ValidDigest(result.snapshotDigest) {
		return result, errs.ErrUnavailable
	}
	parsed, canonical, err := managedkubernetes.DecodeOperationInput(child.Operation, input)
	defer clear(canonical)
	defer clear(parsed.Body)
	defer clear(parsed.Options)
	digest := sha256.Sum256(canonical)
	if err != nil || !bytes.Equal(input, canonical) || hex.EncodeToString(digest[:]) != child.InputDigest {
		return result, errs.ErrUnavailable
	}
	var observation managedkubernetes.ChildCandidate
	if internalrpcauth.DecodeExactJSON(observed, &observation) != nil || observation.Validate() != nil {
		return result, errs.ErrUnavailable
	}
	child.Resource = observation.Resource
	address := child.Resource
	if address.Group != parsed.Group || address.Version != parsed.Version || address.Resource != parsed.Resource || address.Namespace != parsed.Namespace || address.Subresource != parsed.Subresource || address.Name != parsed.Name ||
		parsed.UID != "" && parsed.UID != address.UID || parsed.ResourceVersion != "" && parsed.ResourceVersion != address.ResourceVersion {
		return result, errs.ErrUnavailable
	}
	if child.Operation == "kubernetes.create" {
		if observation.ObservationState != managedkubernetes.ObservationCollection {
			return result, errs.ErrUnavailable
		}
		var body map[string]json.RawMessage
		var metadata map[string]json.RawMessage
		if json.Unmarshal(parsed.Body, &body) != nil || json.Unmarshal(body["metadata"], &metadata) != nil {
			return result, errs.ErrUnavailable
		}
		var name, generateName string
		if raw, ok := metadata["name"]; ok && json.Unmarshal(raw, &name) != nil {
			return result, errs.ErrUnavailable
		}
		if raw, ok := metadata["generateName"]; ok && json.Unmarshal(raw, &generateName) != nil {
			return result, errs.ErrUnavailable
		}
		// Kubernetes selects metadata.name when both source fields exist; generateName is then inert.
		// Kubernetes выбирает metadata.name при обоих source fields; generateName тогда не действует.
		child.IntendedName = name
		if child.IntendedName == "" {
			child.IntendedGenerateName = generateName
		}
	} else if child.Operation == "kubernetes.run" {
		if observation.ObservationState != managedkubernetes.ObservationCollection {
			return result, errs.ErrUnavailable
		}
		var options map[string]json.RawMessage
		if json.Unmarshal(parsed.Options, &options) != nil || json.Unmarshal(options["podName"], &child.IntendedName) != nil || child.IntendedName == "" {
			return result, errs.ErrUnavailable
		}
	} else if observation.ObservationState != managedkubernetes.ObservationPresent {
		return result, errs.ErrUnavailable
	}
	previewOption := kubernetesGatePreviewOptions{budget: managedkubernetes.GatePreviewListBytes}
	if len(previewOptions) > 0 {
		previewOption = previewOptions[0]
	}
	preview, previewErr := managedkubernetes.BuildGateEffectPreview(child.Operation, input, previewOption.disclose, previewOption.budget)
	if previewErr != nil {
		return result, errs.ErrUnavailable
	}
	child.Preview = &preview
	if !child.Valid() {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// projectKubernetesChildGate attaches immutable safe metadata to an already authorized Gate read.
// It does not route through the parent integration preview or issue scoped consent/effect authority.
// projectKubernetesChildGate добавляет immutable safe metadata к уже авторизованному Gate read.
// Не использует parent integration preview и не выдаёт scoped consent/effect authority.
func (repository *Repository) projectKubernetesChildGate(ctx context.Context, runner queryRunner, current scope, gate *entity.OwnerGate, actorScoped bool, modes ...kubernetesGatePreviewMode) (bool, error) {
	_, handled, err := kubernetesChildGateOrigin(ctx, runner, current.organizationID, gate.Ref)
	if err != nil || !handled {
		return handled, err
	}
	options := kubernetesGatePreviewOptions{budget: managedkubernetes.GatePreviewListBytes}
	if actorScoped && len(modes) == 1 && (modes[0] == kubernetesGatePreviewSummary || modes[0] == kubernetesGatePreviewDetail) {
		tx, ok := runner.(pgx.Tx)
		if !ok {
			return true, errs.ErrUnavailable
		}
		accessErr := repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: gate.Ref})
		if accessErr != nil && !errors.Is(accessErr, errs.ErrForbidden) && !errors.Is(accessErr, errs.ErrNotFound) {
			return true, accessErr
		}
		options.disclose = accessErr == nil
		if options.disclose && modes[0] == kubernetesGatePreviewDetail {
			options.budget = managedkubernetes.GatePreviewDetailBytes
		}
	}
	record, err := readKubernetesChildGateRecord(ctx, runner, current.organizationID, gate.Ref, options)
	if err != nil {
		return true, err
	}
	if gate.NodeRef != record.nodeRef {
		return true, errs.ErrUnavailable
	}
	gate.KubernetesChild = &record.summary
	if gate.ExpiresAt == nil || record.deadline.Before(*gate.ExpiresAt) {
		deadline := record.deadline
		gate.ExpiresAt = &deadline
	}
	if !record.deadline.After(record.now) {
		actions := make([]string, 0, len(gate.NextActions))
		for _, action := range gate.NextActions {
			if action != "RESOLVE_GATE" {
				actions = append(actions, action)
			}
		}
		gate.NextActions = actions
	}
	gate.IntegrationIntent = nil
	gate.ResolutionKind = record.resolutionKind
	gate.DecisionConsequences = nil
	for _, decision := range gate.AllowedDecisions {
		consequence := entity.OwnerGateDecisionConsequence{Decision: decision}
		switch decision {
		case "APPROVE":
			consequence.SafeSummary = "i18n:KUBERNETES_CHILD_GATE_APPROVE"
			consequence.ExecutesExternalEffect = true
		case "REJECT":
			consequence.SafeSummary = "i18n:KUBERNETES_CHILD_GATE_REJECT"
		case "CANCEL":
			consequence.SafeSummary = "i18n:KUBERNETES_CHILD_GATE_CANCEL"
		default:
			return true, errs.ErrUnavailable
		}
		gate.DecisionConsequences = append(gate.DecisionConsequences, consequence)
	}
	return true, nil
}
