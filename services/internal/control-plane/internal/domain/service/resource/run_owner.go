package resource

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type runActionDecision struct {
	Cancel bool
	Retry  bool
}

func (decision runActionDecision) actions() []string {
	result := make([]string, 0, 2)
	if decision.Cancel {
		result = append(result, "CANCEL")
	}
	if decision.Retry {
		result = append(result, "RETRY")
	}
	return result
}

// decideRunActionsLocked — единственный предикат projection и command path.
// Он выполняется только после канонического owner-graph lock и закрыто
// отклоняет неизвестный/stale tuple до формирования nextActions.
func (service *Service) decideRunActionsLocked(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	graph lockedOwnerGraph,
) (runActionDecision, error) {
	if graph.Process.Kind != enum.KindProcessRun || graph.Turn.Kind != enum.KindTurn ||
		graph.Process.OwnerActorID != principal.ActorID || graph.Turn.OwnerActorID != principal.ActorID {
		return runActionDecision{}, errs.ErrNotFound
	}
	processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
	turnSpec, turnOK := graph.Turn.Spec.(entity.TurnSpec)
	if !processOK || !turnOK {
		return runActionDecision{}, errs.ErrStateConflict
	}
	current, err := currentExecution(processSpec)
	if err != nil || current.TurnID != graph.Turn.ID || current.TurnVersion != graph.Turn.Version ||
		current.Attempt != turnSpec.Attempt || current.SessionID != turnSpec.SessionID ||
		current.RuntimeRevisionID != turnSpec.RuntimeRevisionID || current.InputSHA256 != turnSpec.EffectiveInputSHA256 {
		return runActionDecision{}, errs.ErrStateConflict
	}
	decision := runActionDecision{}
	var previous domainrepo.TurnAttempt
	previousLoaded := false
	loadPrevious := func() error {
		if previousLoaded {
			return nil
		}
		loaded, loadErr := tx.GetTurnAttemptForUpdate(ctx, graph.Turn.ID, turnSpec.Attempt)
		if loadErr != nil {
			return loadErr
		}
		previous = loaded
		previousLoaded = true
		return nil
	}
	var lease domainrepo.TurnLease
	leaseFound, leaseLoaded := false, false
	loadLease := func() error {
		if leaseLoaded {
			return nil
		}
		loaded, loadErr := tx.GetTurnLeaseForUpdate(ctx, graph.Turn.ID)
		leaseLoaded = true
		if errors.Is(loadErr, errs.ErrNotFound) {
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		lease, leaseFound = loaded, true
		return nil
	}
	if !graph.Process.State.Terminal() && !graph.Turn.State.Terminal() &&
		ownerGraphRuntimeDisposition(graph) == runtimeDispositionAbsent {
		children, childErr := tx.HasActiveChildProcesses(ctx, graph.Process.OrganizationID,
			graph.Process.ProjectID, graph.Process.ID)
		if childErr != nil {
			return runActionDecision{}, childErr
		}
		turns, turnErr := tx.ActiveProcessTurnCandidates(ctx, graph.Process.OrganizationID,
			graph.Process.ProjectID, graph.Process.ID)
		if turnErr != nil {
			return runActionDecision{}, turnErr
		}
		candidate := !children && len(turns) == 1 && turns[0].ID == graph.Turn.ID &&
			turns[0].Version == graph.Turn.Version
		if candidate {
			if err := loadPrevious(); err != nil {
				if !errors.Is(err, errs.ErrNotFound) {
					return runActionDecision{}, err
				}
				candidate = false
			}
		}
		if candidate && (previous.InputSHA256 != turnSpec.EffectiveInputSHA256 ||
			!previous.FinishedAt.IsZero()) {
			candidate = false
		}
		if candidate {
			if err := loadLease(); err != nil {
				return runActionDecision{}, err
			}
			if leaseFound && (lease.Attempt != turnSpec.Attempt || lease.Fence != graph.Turn.Version ||
				lease.AuthorityGeneration != previous.AuthorityGeneration) {
				candidate = false
			}
		}
		decision.Cancel = candidate
	}
	if graph.Process.State.Terminal() || !slices.Contains([]enum.State{enum.StateFailed, enum.StateBlocked,
		enum.StateWaitingOwner, enum.StateCancelled}, graph.Turn.State) {
		return decision, nil
	}
	if graph.Runtime != nil && (graph.Runtime.State != "SUSPENDED" || graph.Turn.State != enum.StateWaitingOwner) {
		return decision, nil
	}
	if err := requireOwnerGraphRuntimeDisposition(graph, runtimeDispositionAbsent, runtimeDispositionTerminal); err != nil {
		return decision, nil
	}
	err = loadPrevious()
	if err != nil || previous.InputSHA256 != turnSpec.EffectiveInputSHA256 {
		if err != nil && !errors.Is(err, errs.ErrNotFound) {
			return runActionDecision{}, err
		}
		return decision, nil
	}
	switch graph.Turn.State {
	case enum.StateFailed:
		decision.Retry = !previous.FinishedAt.IsZero()
	case enum.StateCancelled:
		decision.Retry = previous.State == string(enum.StateCancelled) && !previous.FinishedAt.IsZero()
	case enum.StateWaitingOwner:
		decision.Retry = previous.State == "WAITING_OWNER" && !previous.FinishedAt.IsZero() &&
			previous.Outcome == "owner_gate_pending"
	case enum.StateBlocked:
		decision.Retry = previous.FinishedAt.IsZero()
	}
	if !decision.Retry {
		return decision, nil
	}
	leaseErr := loadLease()
	if leaseErr != nil {
		return runActionDecision{}, leaseErr
	}
	if leaseFound {
		if graph.Turn.State == enum.StateWaitingOwner || lease.Attempt != turnSpec.Attempt ||
			lease.AuthorityGeneration != previous.AuthorityGeneration {
			decision.Retry = false
		}
	}
	return decision, nil
}

// ManageRun маршрутизирует закрытые cancel/retry действия в уже авторитетные
// owner graph команды, не вводя второй lifecycle path.
func (service *Service) ManageRun(ctx context.Context, input ManageRunInput) (ManageRunResult, error) {
	if err := authorize(input.Principal, permissionRunManage); err != nil {
		return ManageRunResult{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ProcessRunID) != nil || input.ExpectedVersion == 0 ||
		value.ValidateStableKey(input.ReasonCode) != nil ||
		(input.Action != "cancel" && input.Action != "retry") {
		return ManageRunResult{}, errs.ErrInvalidInput
	}
	process, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ProcessRunID, enum.KindProcessRun)
	if err != nil {
		return ManageRunResult{}, err
	}
	if process.OwnerActorID != input.Principal.ActorID {
		return ManageRunResult{}, errs.ErrNotFound
	}
	if input.Action == "cancel" {
		if process.Version != input.ExpectedVersion {
			return ManageRunResult{}, errs.ErrVersionMismatch
		}
		principal := input.Principal
		principal.Permission = permissionCancelProcess
		cancelled, err := service.CancelProcess(ctx, CancelProcessInput{Principal: principal,
			IdempotencyKey: input.IdempotencyKey, ProcessRunID: input.ProcessRunID,
			ExpectedVersion: input.ExpectedVersion, ReasonCode: input.ReasonCode})
		return ManageRunResult{ProcessRun: cancelled}, err
	}
	if process.Version != input.ExpectedVersion {
		return ManageRunResult{}, errs.ErrVersionMismatch
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return ManageRunResult{}, errs.ErrStateConflict
	}
	current, err := currentExecution(spec)
	if err != nil || current.TurnID == "" {
		return ManageRunResult{}, errs.ErrStateConflict
	}
	turn, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		current.TurnID, enum.KindTurn)
	if err != nil || turn.OwnerActorID != input.Principal.ActorID || turn.Version != current.TurnVersion {
		if err != nil {
			return ManageRunResult{}, err
		}
		return ManageRunResult{}, errs.ErrStateConflict
	}
	principal := input.Principal
	principal.Permission = permissionRetryTurn
	retried, err := service.RetryTurn(ctx, RetryTurnInput{Principal: principal,
		IdempotencyKey: input.IdempotencyKey, TurnID: turn.ID, ExpectedVersion: turn.Version,
		ReasonCode: input.ReasonCode})
	if err != nil {
		return ManageRunResult{}, err
	}
	updated, err := service.repository.Get(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.ProcessRunID, enum.KindProcessRun)
	if err != nil {
		return ManageRunResult{}, err
	}
	return ManageRunResult{ProcessRun: updated, SuccessorTurn: retried}, nil
}

func (service *Service) GetRunDetail(
	ctx context.Context,
	principal value.Principal,
	processRunID string,
) (RunDetailResult, error) {
	return service.GetRunDetailPage(ctx, principal, processRunID, 100, "")
}

type runSnapshotCursor struct {
	Kind            string    `json:"kind"`
	ProcessRunID    string    `json:"processRunId"`
	ProcessVersion  uint64    `json:"processVersion"`
	SnapshotSHA256  string    `json:"snapshotSha256,omitempty"`
	ExecutionID     string    `json:"executionId,omitempty"`
	AfterType       string    `json:"afterType,omitempty"`
	AfterID         string    `json:"afterId"`
	AfterOccurredAt time.Time `json:"afterOccurredAt,omitempty"`
}

func (service *Service) encodeRunSnapshotCursor(cursor runSnapshotCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", errs.ErrInternal
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write([]byte("control-plane:owner-run-page:v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func (service *Service) decodeRunSnapshotCursor(token string) (runSnapshotCursor, error) {
	if token == "" {
		return runSnapshotCursor{}, nil
	}
	if len(token) > 2048 {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := hex.DecodeString(parts[1])
	if err != nil || signatureErr != nil {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	mac := hmac.New(sha256.New, service.leaseSigningKey)
	_, _ = mac.Write([]byte("control-plane:owner-run-page:v1\x00"))
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return runSnapshotCursor{}, errs.ErrStateConflict
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var cursor runSnapshotCursor
	if err := decoder.Decode(&cursor); err != nil {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	listCursor := slices.Contains([]string{"AGENT_LIST", "CATALOG_LIST", "SCHEDULE_LIST", "RUN_LIST",
		"INCIDENT_LIST", "RESTORE_LIST"}, cursor.Kind)
	if (!listCursor && cursor.Kind != "INCIDENT" && cursor.Kind != "LINEAGE" && cursor.Kind != "TIMELINE") ||
		(!listCursor && (value.ValidateID(cursor.ProcessRunID) != nil || cursor.ProcessVersion == 0)) ||
		(listCursor && (cursor.ProcessRunID != "" || cursor.ProcessVersion != 0 ||
			!validSHA256Text(cursor.SnapshotSHA256))) ||
		value.ValidateID(cursor.AfterID) != nil ||
		(cursor.ExecutionID != "" && value.ValidateID(cursor.ExecutionID) != nil) ||
		(cursor.AfterType != "" && cursor.AfterType != "PROCESS" && cursor.AfterType != "ATTEMPT") {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	if (cursor.Kind == "TIMELINE") != !cursor.AfterOccurredAt.IsZero() {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runSnapshotCursor{}, errs.ErrInvalidInput
	}
	return cursor, nil
}

func (service *Service) decodeOwnerListCursor(token, kind string) (runSnapshotCursor, error) {
	if token == "" {
		return runSnapshotCursor{Kind: kind}, nil
	}
	cursor, err := service.decodeRunSnapshotCursor(token)
	if err != nil || cursor.Kind != kind {
		return runSnapshotCursor{}, errs.ErrStateConflict
	}
	return cursor, nil
}

func ownerListSnapshot(ctx context.Context, tx domainrepo.Transaction, expectedSHA256 string) (string, error) {
	ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
	if !ok {
		return "", errs.ErrInternal
	}
	snapshot, err := ownerRead.OwnerSnapshotFence(ctx)
	if err != nil {
		return "", err
	}
	digest := hashString(snapshot)
	if expectedSHA256 != "" && expectedSHA256 != digest {
		return "", errs.ErrVersionMismatch
	}
	return digest, nil
}

// GetRunDetailPage возвращает Run, actions и exact execution incidents из
// одного serializable owner snapshot; cursor привязан к версии ProcessRun.
func (service *Service) GetRunDetailPage(
	ctx context.Context,
	principal value.Principal,
	processRunID string,
	incidentLimit int,
	incidentPageToken string,
) (RunDetailResult, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return RunDetailResult{}, err
	}
	if value.ValidateID(processRunID) != nil || incidentLimit < 1 || incidentLimit > 100 {
		return RunDetailResult{}, errs.ErrInvalidInput
	}
	cursor, err := service.decodeRunSnapshotCursor(incidentPageToken)
	if err != nil || (incidentPageToken != "" && (cursor.Kind != "INCIDENT" || cursor.ProcessRunID != processRunID)) {
		return RunDetailResult{}, errs.ErrStateConflict
	}
	var result RunDetailResult
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		graph, graphErr := service.lockOwnerGraphByProcess(ctx, tx, principal, processRunID)
		if graphErr != nil {
			return graphErr
		}
		if cursor.ProcessVersion != 0 && cursor.ProcessVersion != graph.Process.Version {
			return errs.ErrVersionMismatch
		}
		processSpec, ok := graph.Process.Spec.(entity.ProcessRunSpec)
		if !ok {
			return errs.ErrStateConflict
		}
		tuple, tupleErr := currentExecution(processSpec)
		if tupleErr != nil {
			return tupleErr
		}
		revision, revisionErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, tuple.RuntimeRevisionID)
		if revisionErr != nil || revision.Kind != enum.KindRuntimeRevision ||
			revision.OwnerActorID != principal.ActorID || revision.Version != tuple.RuntimeRevisionVersion {
			if revisionErr != nil {
				return revisionErr
			}
			return errs.ErrStateConflict
		}
		result = RunDetailResult{ProcessRun: graph.Process, Session: graph.Session,
			Turn: graph.Turn, RuntimeRevision: revision, Runtime: graph.Runtime}
		decision, decisionErr := service.decideRunActionsLocked(ctx, tx, principal, graph)
		if decisionErr != nil {
			return decisionErr
		}
		result.Projection, decisionErr = service.runOwnerProjectionFromTx(ctx, tx, principal, result, decision)
		if decisionErr != nil {
			return decisionErr
		}
		if graph.Runtime == nil {
			if cursor.ExecutionID != "" {
				return errs.ErrVersionMismatch
			}
			return nil
		}
		if cursor.ExecutionID != "" && cursor.ExecutionID != graph.Runtime.ID {
			return errs.ErrVersionMismatch
		}
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		incidents, incidentErr := ownerRead.ListRuntimeIncidentsSnapshot(ctx, query.RuntimeIncidentFilter{
			OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
			ActorID: principal.ActorID, ExecutionID: graph.Runtime.ID, AfterID: cursor.AfterID,
			Limit: incidentLimit + 1,
		})
		if incidentErr != nil {
			return incidentErr
		}
		truncated := len(incidents) > incidentLimit
		if truncated {
			incidents = incidents[:incidentLimit]
		}
		result.Incidents = incidents
		result.IncidentProjections = make([]RuntimeIncidentOwnerProjection, 0, len(incidents))
		for _, incident := range incidents {
			projection, projectionErr := service.runtimeIncidentOwnerProjectionFromTx(ctx, tx, principal, incident)
			if projectionErr != nil {
				return projectionErr
			}
			result.IncidentProjections = append(result.IncidentProjections, projection)
		}
		if truncated {
			result.IncidentsNextPageToken, incidentErr = service.encodeRunSnapshotCursor(runSnapshotCursor{
				Kind: "INCIDENT", ProcessRunID: graph.Process.ID, ProcessVersion: graph.Process.Version,
				ExecutionID: graph.Runtime.ID, AfterID: incidents[len(incidents)-1].ID,
			})
		}
		return incidentErr
	})
	return result, err
}

func (service *Service) GetRunLineage(ctx context.Context, principal value.Principal, processRunID string) (RunLineageResult, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return RunLineageResult{}, err
	}
	process, tuple, err := service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return RunLineageResult{}, err
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return RunLineageResult{}, errs.ErrStateConflict
	}
	graph, err := service.resolveRunGraph(ctx, principal, processRunID)
	if err != nil {
		return RunLineageResult{}, err
	}
	result := RunLineageResult{RootSessionID: spec.RootSessionID, RootTurnID: spec.RootTurnID,
		ParentProcessRunID: spec.ParentProcessRunID, CurrentSessionID: tuple.SessionID,
		CurrentSessionVersion: tuple.SessionVersion, CurrentTurnID: tuple.TurnID,
		CurrentTurnVersion: tuple.TurnVersion, CurrentAttempt: tuple.Attempt,
		RuntimeRevisionID: tuple.RuntimeRevisionID, RuntimeRevisionVersion: tuple.RuntimeRevisionVersion,
		ImmutableInputSHA256: tuple.InputSHA256, Complete: true}
	for _, node := range graph {
		switch node.NodeType {
		case "PROCESS":
			result.Processes = append(result.Processes, node)
			if !enum.State(node.State).Terminal() {
				result.Complete = false
			}
			if node.ParentProcessRunID == "" {
				result.RootProcessRunID = node.ID
			}
		case "ATTEMPT":
			result.Attempts = append(result.Attempts, node)
			if !enum.State(node.State).Terminal() {
				result.Complete = false
			}
		}
	}
	linkRunLineage(&result)
	if result.RootProcessRunID == "" || len(result.Processes) == 0 {
		return RunLineageResult{}, errs.ErrStateConflict
	}
	return result, nil
}

// GetRunLineagePage ограничивает обход до SQL allocation, возвращает
// snapshot-bound continuation и никогда не помечает неполную страницу полной.
func (service *Service) GetRunLineagePage(
	ctx context.Context,
	principal value.Principal,
	processRunID string,
	limit int,
	pageToken string,
) (RunLineageOwnerPage, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return RunLineageOwnerPage{}, err
	}
	if value.ValidateID(processRunID) != nil || limit < 1 || limit > 100 {
		return RunLineageOwnerPage{}, errs.ErrInvalidInput
	}
	cursor, err := service.decodeRunSnapshotCursor(pageToken)
	if err != nil || (pageToken != "" && (cursor.Kind != "LINEAGE" || cursor.ProcessRunID != processRunID)) {
		return RunLineageOwnerPage{}, errs.ErrStateConflict
	}
	var result RunLineageOwnerPage
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		graph, graphErr := service.lockOwnerGraphByProcess(ctx, tx, principal, processRunID)
		if graphErr != nil {
			return graphErr
		}
		if cursor.ProcessVersion != 0 && cursor.ProcessVersion != graph.Process.Version {
			return errs.ErrVersionMismatch
		}
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		nodes, truncated, nodeErr := ownerRead.ListRunGraphNodesPage(ctx, processRunID,
			cursor.AfterType, cursor.AfterID, limit)
		if nodeErr != nil {
			return nodeErr
		}
		foundRequested := false
		lineage := RunLineageResult{Complete: !truncated}
		for _, node := range nodes {
			switch node.NodeType {
			case "PROCESS":
				lineage.Processes = append(lineage.Processes, node)
				if node.ID == processRunID {
					foundRequested = true
				}
			case "ATTEMPT":
				lineage.Attempts = append(lineage.Attempts, node)
			default:
				return errs.ErrStateConflict
			}
		}
		if pageToken == "" && !foundRequested {
			return errs.ErrNotFound
		}
		projections, projectionErr := RunLineageOwnerProjections(lineage)
		if projectionErr != nil {
			return projectionErr
		}
		projectionByNodeID := make(map[string]int, len(projections))
		for index, node := range lineage.Processes {
			projectionByNodeID[node.ID] = index
		}
		for index, node := range lineage.Attempts {
			projectionByNodeID[node.ID] = len(lineage.Processes) + index
		}
		for _, node := range nodes {
			if node.RuntimeRevisionID == "" {
				continue
			}
			index, found := projectionByNodeID[node.ID]
			if !found {
				return errs.ErrStateConflict
			}
			agent, role, model, provider, displayErr := service.runRevisionOwnerDisplaysFromTx(ctx, tx,
				principal, node.RuntimeRevisionID, node.RuntimeRevisionVersion)
			if displayErr != nil {
				return displayErr
			}
			projections[index].Agent, projections[index].Role = agent, role
			projections[index].Model, projections[index].Provider = model, provider
		}
		processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
		if !processOK {
			return errs.ErrStateConflict
		}
		tuple, tupleErr := currentExecution(processSpec)
		if tupleErr != nil {
			return tupleErr
		}
		revision, revisionErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID,
			tuple.RuntimeRevisionID)
		if revisionErr != nil || revision.Version != tuple.RuntimeRevisionVersion {
			if revisionErr != nil {
				return revisionErr
			}
			return errs.ErrVersionMismatch
		}
		decision, decisionErr := service.decideRunActionsLocked(ctx, tx, principal, graph)
		if decisionErr != nil {
			return decisionErr
		}
		run, projectionErr := service.runOwnerProjectionFromTx(ctx, tx, principal,
			RunDetailResult{ProcessRun: graph.Process, Session: graph.Session, Turn: graph.Turn,
				RuntimeRevision: revision, Runtime: graph.Runtime}, decision)
		if projectionErr != nil {
			return projectionErr
		}
		result = RunLineageOwnerPage{Lineage: lineage, Projections: projections, Run: run, Truncated: truncated}
		if truncated {
			last := nodes[len(nodes)-1]
			result.NextPageToken, nodeErr = service.encodeRunSnapshotCursor(runSnapshotCursor{
				Kind: "LINEAGE", ProcessRunID: graph.Process.ID, ProcessVersion: graph.Process.Version,
				AfterType: last.NodeType, AfterID: last.ID,
			})
		}
		return nodeErr
	})
	return result, err
}

func (service *Service) runRevisionOwnerDisplaysFromTx(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	revisionID string,
	expectedVersion uint64,
) (OwnerDisplayValue, OwnerDisplayValue, OwnerDisplayValue, OwnerDisplayValue, error) {
	unavailable := OwnerDisplayValue{Status: OwnerProjectionUnavailable}
	revision, err := tx.Get(ctx, principal.OrganizationID, principal.ProjectID, revisionID)
	if err != nil {
		return unavailable, unavailable, unavailable, unavailable, err
	}
	if revision.Kind != enum.KindRuntimeRevision || revision.OwnerActorID != principal.ActorID ||
		(expectedVersion != 0 && revision.Version != expectedVersion) {
		return unavailable, unavailable, unavailable, unavailable, errs.ErrVersionMismatch
	}
	spec, ok := revision.Spec.(entity.RuntimeRevisionSpec)
	if !ok {
		return unavailable, unavailable, unavailable, unavailable, errs.ErrStateConflict
	}
	agent, role, model, provider := unavailable, unavailable, unavailable, unavailable
	if validOwnerRunDisplay(spec.CodexModel) {
		model = OwnerDisplayValue{Status: OwnerProjectionPresent, Value: spec.CodexModel}
	}
	if spec.AgentID != "" {
		agent, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindAgent,
			spec.AgentID, spec.AgentVersion, spec.AgentSHA256, false)
		if err != nil {
			return unavailable, unavailable, unavailable, unavailable, err
		}
	}
	if spec.RoleDefinitionID != "" {
		role, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindRoleDefinition,
			spec.RoleDefinitionID, spec.RoleDefinitionVersion, spec.RoleDefinitionSHA256, false)
		if err != nil {
			return unavailable, unavailable, unavailable, unavailable, err
		}
	}
	if spec.ProviderPoolID != "" {
		provider, err = ownerPinnedRunDisplay(ctx, tx, principal, enum.KindProviderPool,
			spec.ProviderPoolID, spec.ProviderPoolVersion, spec.ProviderPoolSHA256, true)
		if err != nil {
			return unavailable, unavailable, unavailable, unavailable, err
		}
	}
	return agent, role, model, provider, nil
}

func linkRunLineage(result *RunLineageResult) {
	processIndexes := make(map[string]int, len(result.Processes))
	for index := range result.Processes {
		processIndexes[result.Processes[index].ID] = index
	}
	for index := range result.Processes {
		parentID := result.Processes[index].ParentProcessRunID
		if parentIndex, ok := processIndexes[parentID]; ok {
			result.Processes[parentIndex].ChildIDs = append(
				result.Processes[parentIndex].ChildIDs,
				result.Processes[index].ID,
			)
		}
	}
	for index := range result.Processes {
		sort.Strings(result.Processes[index].ChildIDs)
	}
	attemptIndexes := make(map[string][]int, len(result.Attempts))
	for index := range result.Attempts {
		attemptIndexes[result.Attempts[index].TurnID] = append(attemptIndexes[result.Attempts[index].TurnID], index)
	}
	for _, indexes := range attemptIndexes {
		sort.Slice(indexes, func(left, right int) bool {
			leftAttempt, rightAttempt := result.Attempts[indexes[left]], result.Attempts[indexes[right]]
			if leftAttempt.Attempt == rightAttempt.Attempt {
				return leftAttempt.ID < rightAttempt.ID
			}
			return leftAttempt.Attempt < rightAttempt.Attempt
		})
		for position, index := range indexes {
			if position > 0 {
				result.Attempts[index].PredecessorID = result.Attempts[indexes[position-1]].ID
				result.Attempts[indexes[position-1]].SuccessorID = result.Attempts[index].ID
				continue
			}
			predecessorTurnID := result.Attempts[index].PredecessorID
			predecessors := attemptIndexes[predecessorTurnID]
			if len(predecessors) != 0 {
				sort.Slice(predecessors, func(left, right int) bool {
					return result.Attempts[predecessors[left]].Attempt < result.Attempts[predecessors[right]].Attempt
				})
				predecessor := predecessors[len(predecessors)-1]
				result.Attempts[index].PredecessorID = result.Attempts[predecessor].ID
				result.Attempts[predecessor].SuccessorID = result.Attempts[index].ID
			}
		}
	}
}

func (service *Service) resolveRunGraph(ctx context.Context, principal value.Principal,
	processRunID string,
) ([]domainrepo.RunGraphNode, error) {
	if value.ValidateID(processRunID) != nil {
		return nil, errs.ErrInvalidInput
	}
	repository, ok := service.repository.(domainrepo.RunGraphRepository)
	if !ok {
		return nil, errs.ErrUnavailable
	}
	nodes, err := repository.ListRunGraphNodes(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, processRunID)
	if err != nil {
		return nil, err
	}
	foundRequested := false
	for _, node := range nodes {
		if node.NodeType == "PROCESS" && node.ID == processRunID {
			foundRequested = true
		}
	}
	if !foundRequested {
		return nil, errs.ErrNotFound
	}
	return nodes, nil
}

func (service *Service) resolveRun(ctx context.Context, principal value.Principal, processRunID string) (entity.Resource, executionTuple, error) {
	if value.ValidateID(processRunID) != nil {
		return entity.Resource{}, executionTuple{}, errs.ErrInvalidInput
	}
	process, err := service.repository.Get(ctx, principal.OrganizationID, principal.ProjectID, processRunID, enum.KindProcessRun)
	if err != nil {
		return entity.Resource{}, executionTuple{}, err
	}
	if process.OwnerActorID != principal.ActorID {
		return entity.Resource{}, executionTuple{}, errs.ErrNotFound
	}
	spec, ok := process.Spec.(entity.ProcessRunSpec)
	if !ok {
		return entity.Resource{}, executionTuple{}, errs.ErrStateConflict
	}
	tuple, err := currentExecution(spec)
	if err != nil || tuple.SessionID == "" || tuple.TurnID == "" {
		return entity.Resource{}, executionTuple{}, errs.ErrStateConflict
	}
	return process, tuple, nil
}

type runTimelineCursor struct {
	OccurredAt time.Time `json:"occurredAt"`
	ID         string    `json:"id"`
}

func (service *Service) ListRunTimelineOwner(
	ctx context.Context,
	principal value.Principal,
	processRunID, pageToken string,
	limit int,
) (RunTimelineOwnerPage, error) {
	if err := authorize(principal, permissionAuditRead); err != nil {
		return RunTimelineOwnerPage{}, err
	}
	if value.ValidateID(processRunID) != nil || limit < 1 || limit > 100 {
		return RunTimelineOwnerPage{}, errs.ErrInvalidInput
	}
	cursor, err := service.decodeRunSnapshotCursor(pageToken)
	if err != nil || (pageToken != "" && (cursor.Kind != "TIMELINE" || cursor.ProcessRunID != processRunID)) {
		return RunTimelineOwnerPage{}, errs.ErrStateConflict
	}
	var result RunTimelineOwnerPage
	err = service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID}, func(tx domainrepo.Transaction) error {
		graph, graphErr := service.lockOwnerGraphByProcess(ctx, tx, principal, processRunID)
		if graphErr != nil {
			return graphErr
		}
		if cursor.ProcessVersion != 0 && cursor.ProcessVersion != graph.Process.Version {
			return errs.ErrVersionMismatch
		}
		ownerRead, ok := tx.(domainrepo.OwnerReadTransaction)
		if !ok {
			return errs.ErrInternal
		}
		nodes, graphTruncated, graphErr := ownerRead.ListRunGraphNodesPage(ctx, processRunID, "", "", 1000)
		if graphErr != nil || graphTruncated {
			if graphErr != nil {
				return graphErr
			}
			return errs.ErrStateConflict
		}
		ids := make([]string, 0, len(nodes)*5)
		for _, node := range nodes {
			ids = append(ids, node.ID)
			for _, id := range []string{node.ProcessRunID, node.SessionID, node.TurnID, node.RuntimeRevisionID} {
				if id != "" {
					ids = append(ids, id)
				}
			}
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		items, listErr := ownerRead.ListRunTimelineAuditSnapshot(ctx, ids,
			cursor.AfterOccurredAt, cursor.AfterID, limit+1)
		if listErr != nil {
			return listErr
		}
		truncated := len(items) > limit
		if truncated {
			items = items[:limit]
		}
		processSpec, processOK := graph.Process.Spec.(entity.ProcessRunSpec)
		if !processOK {
			return errs.ErrStateConflict
		}
		tuple, tupleErr := currentExecution(processSpec)
		if tupleErr != nil {
			return tupleErr
		}
		revision, revisionErr := tx.Get(ctx, principal.OrganizationID, principal.ProjectID,
			tuple.RuntimeRevisionID)
		if revisionErr != nil || revision.Version != tuple.RuntimeRevisionVersion {
			if revisionErr != nil {
				return revisionErr
			}
			return errs.ErrVersionMismatch
		}
		decision, decisionErr := service.decideRunActionsLocked(ctx, tx, principal, graph)
		if decisionErr != nil {
			return decisionErr
		}
		result.Run, decisionErr = service.runOwnerProjectionFromTx(ctx, tx, principal,
			RunDetailResult{ProcessRun: graph.Process, Session: graph.Session, Turn: graph.Turn,
				RuntimeRevision: revision, Runtime: graph.Runtime}, decision)
		if decisionErr != nil {
			return decisionErr
		}
		result.Projections = RunTimelineOwnerProjections(items, result.Run.NextActions)
		if truncated {
			last := items[len(items)-1]
			result.NextPageToken, listErr = service.encodeRunSnapshotCursor(runSnapshotCursor{
				Kind: "TIMELINE", ProcessRunID: graph.Process.ID, ProcessVersion: graph.Process.Version,
				AfterID: last.ID, AfterOccurredAt: last.OccurredAt,
			})
		}
		return listErr
	})
	return result, err
}

func (service *Service) ListRunTimeline(
	ctx context.Context,
	principal value.Principal,
	processRunID, pageToken string,
	limit int,
) ([]domainrepo.Audit, string, error) {
	if err := authorize(principal, permissionAuditRead); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		return nil, "", errs.ErrInvalidInput
	}
	cursor, err := decodeRunTimelineCursor(pageToken)
	if err != nil {
		return nil, "", err
	}
	_, _, err = service.resolveRun(ctx, principal, processRunID)
	if err != nil {
		return nil, "", err
	}
	graph, err := service.resolveRunGraph(ctx, principal, processRunID)
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(graph)*5)
	for _, node := range graph {
		ids = append(ids, node.ID)
		for _, id := range []string{node.ProcessRunID, node.SessionID, node.TurnID, node.RuntimeRevisionID} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	repository, ok := service.repository.(domainrepo.RunTimelineRepository)
	if !ok {
		return nil, "", errs.ErrInternal
	}
	result, err := repository.ListRunTimelineAudit(
		ctx, principal.OrganizationID, principal.ProjectID, principal.ActorID,
		ids, cursor.OccurredAt, cursor.ID, limit,
	)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(result) == limit {
		next, err = encodeRunTimelineCursor(runTimelineCursor{
			OccurredAt: result[len(result)-1].OccurredAt,
			ID:         result[len(result)-1].ID,
		})
		if err != nil {
			return nil, "", err
		}
	}
	return result, next, nil
}

func decodeRunTimelineCursor(token string) (runTimelineCursor, error) {
	if token == "" {
		return runTimelineCursor{}, nil
	}
	if len(token) > 512 {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var cursor runTimelineCursor
	if err := decoder.Decode(&cursor); err != nil || cursor.OccurredAt.IsZero() ||
		value.ValidateID(cursor.ID) != nil {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runTimelineCursor{}, errs.ErrInvalidInput
	}
	return cursor, nil
}

func encodeRunTimelineCursor(cursor runTimelineCursor) (string, error) {
	if cursor.OccurredAt.IsZero() || value.ValidateID(cursor.ID) != nil {
		return "", errs.ErrInternal
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", errs.ErrInternal
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (service *Service) ListRunArtifacts(ctx context.Context, principal value.Principal, processRunID, pageToken string, limit int) ([]entity.Resource, string, error) {
	if err := authorize(principal, permissionRead); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 100 {
		return nil, "", errs.ErrInvalidInput
	}
	cursor, err := decodeRunTimelineCursor(pageToken)
	if err != nil {
		return nil, "", err
	}
	if _, _, err := service.resolveRun(ctx, principal, processRunID); err != nil {
		return nil, "", err
	}
	repository, ok := service.repository.(domainrepo.RunGraphRepository)
	if !ok {
		return nil, "", errs.ErrUnavailable
	}
	found, err := repository.ListRunGraphArtifacts(ctx, principal.OrganizationID, principal.ProjectID,
		principal.ActorID, processRunID, cursor.OccurredAt, cursor.ID, limit+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(found) > limit {
		found = found[:limit]
		last := found[len(found)-1]
		next, err = encodeRunTimelineCursor(runTimelineCursor{OccurredAt: last.CreatedAt, ID: last.ID})
		if err != nil {
			return nil, "", err
		}
	}
	return filterOwnerBoundResources(found, principal.ActorID), next, nil
}
