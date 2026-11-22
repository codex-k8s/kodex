package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesPortForwardProfile reads original observed protocol and explicit concurrent capacity without target credentials.
// queryKubernetesPortForwardProfile читает original observed protocol и explicit concurrent capacity без target credentials.
//
//go:embed sql/kubernetes_pf__profile.sql
var queryKubernetesPortForwardProfile string

// queryKubernetesPortForwardRead resolves one exact actor-owned row inside the already locked original stream.
// queryKubernetesPortForwardRead разрешает exact actor-owned row внутри already locked original stream.
//
//go:embed sql/kubernetes_pf__connection_read.sql
var queryKubernetesPortForwardRead string

// queryKubernetesPortForwardList orders bounded history by its immutable owner generation within one stream.
// queryKubernetesPortForwardList упорядочивает bounded history по immutable owner generation внутри одного stream.
//
//go:embed sql/kubernetes_pf__connection_list.sql
var queryKubernetesPortForwardList string

// queryKubernetesPortForwardGeneration advances only the persistent original stream counter under its lock.
// queryKubernetesPortForwardGeneration продвигает лишь persistent original stream counter под его lock.
//
//go:embed sql/kubernetes_pf__generation.sql
var queryKubernetesPortForwardGeneration string

// queryKubernetesPortForwardInsert connects one server incarnation to its committed private OPEN stage.
// queryKubernetesPortForwardInsert связывает server incarnation с committed private OPEN stage.
//
//go:embed sql/kubernetes_pf__connection_insert.sql
var queryKubernetesPortForwardInsert string

// queryKubernetesPortForwardInputBind seals one canonical action to the existing immutable private input tuple.
// queryKubernetesPortForwardInputBind закрепляет canonical action за existing immutable private input tuple.
//
//go:embed sql/kubernetes_pf__input_bind.sql
var queryKubernetesPortForwardInputBind string

// queryKubernetesPortForwardClose closes one owner incarnation without claiming native join or canceling its siblings.
// queryKubernetesPortForwardClose закрывает owner incarnation без утверждения native join либо отмены siblings.
//
//go:embed sql/kubernetes_pf__connection_close.sql
var queryKubernetesPortForwardClose string

// kubernetesPortForwardRecord retains immutable owner identity and known admission facts without any TCP payload.
// kubernetesPortForwardRecord хранит immutable owner identity и known admission facts без TCP payload.
type kubernetesPortForwardRecord struct {
	id, openKey, openIntent, openStage, closeStage, closeKey string
	expectedVersion                                          int64
	admitted, acknowledged                                   bool
	view                                                     entity.KubernetesPortForwardConnection
}

// readKubernetesPortForwardConnection preserves owner closure and native join as separate safe facts under the common original-root lock.
// readKubernetesPortForwardConnection сохраняет owner closure и native join как отдельные safe facts под common original-root lock.
func readKubernetesPortForwardConnection(ctx context.Context, tx pgx.Tx, current scope, parent kubernetesInputRecord, ref, key string) (kubernetesPortForwardRecord, error) {
	var row kubernetesPortForwardRecord
	err := tx.QueryRow(ctx, queryKubernetesPortForwardRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id, "actor_id": current.actorID, "connection_ref": ref, "open_key": key}).Scan(&row.id, &row.view.Ref, &row.view.Generation, &row.view.Slot, &row.view.State, &row.view.ReasonCode, &row.view.LocalWriteClosed, &row.view.ReadEndObserved, &row.view.NativeJoined, &row.openKey, &row.openIntent, &row.expectedVersion, &row.openStage, &row.closeStage, &row.closeKey, &row.admitted, &row.acknowledged)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	profile, decodeErr := managedkubernetes.DecodePortForwardProfile(parent.input)
	if parent.view.Operation != "kubernetes.port-forward" || decodeErr != nil || !managedkubernetes.ValidPortForwardConnectionRef(row.view.Ref) || row.view.Generation < 1 || int(row.view.Slot) >= len(profile.Ports) || !row.view.State.Known() || row.view.ReasonCode != "" && !managedkubernetes.ValidReasonCode(row.view.ReasonCode) {
		return kubernetesPortForwardRecord{}, errs.ErrUnavailable
	}
	row.view.InvocationRef, row.view.StreamRef = parent.view.InvocationRef, parent.view.StreamRef
	row.view.StreamGeneration, row.view.InputVersion = parent.view.Generation, parent.view.Version
	row.view.HardDeadline = parent.view.HardDeadline
	return row, nil
}

// readKubernetesPortForwardProfile reports selected observed protocol/capacity, never full target readiness or a fresh action grant.
// readKubernetesPortForwardProfile сообщает selected observed protocol/capacity, не full target readiness либо fresh action grant.
func readKubernetesPortForwardProfile(ctx context.Context, tx pgx.Tx, current scope, parent kubernetesInputRecord) (entity.KubernetesPortForwardConnectionProfile, error) {
	profile := entity.KubernetesPortForwardConnectionProfile{MaximumActiveConnections: managedkubernetes.MaximumPortForwardConnections}
	selected, decodeErr := managedkubernetes.DecodePortForwardProfile(parent.input)
	if decodeErr != nil {
		return profile, errs.ErrUnavailable
	}
	profile.Ports = make([]managedkubernetes.ForwardPort, len(selected.Ports))
	for i, port := range selected.Ports {
		profile.Ports[i] = managedkubernetes.ForwardPort{Port: port.Port, TargetPort: port.TargetPort}
	}
	profile.TimeoutSeconds = selected.TimeoutSeconds
	var generation int64
	if parent.view.Operation != "kubernetes.port-forward" {
		return profile, errs.ErrForbidden
	}
	if err := tx.QueryRow(ctx, queryKubernetesPortForwardProfile, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id}).Scan(&profile.Protocol, &generation, &profile.ActiveConnections); err != nil {
		return profile, errs.ErrUnavailable
	}
	if profile.ActiveConnections > profile.MaximumActiveConnections || generation < 0 {
		return profile, errs.ErrUnavailable
	}
	switch profile.Protocol {
	case string(managedkubernetes.PortForwardWebSocketV4):
		profile.AdmissionReasonCode = managedkubernetes.ReasonPortForwardProfileUnsupported
		return profile, nil
	case "":
		profile.AdmissionReasonCode = "KUBERNETES_RECONCILE_REQUIRED"
		return profile, nil
	case string(managedkubernetes.PortForwardSPDYV1):
	default:
		return profile, errs.ErrUnavailable
	}
	if parent.view.State != "ACTIVE" {
		profile.AdmissionReasonCode = "KUBERNETES_RECONCILE_REQUIRED"
	} else if profile.ActiveConnections >= profile.MaximumActiveConnections {
		profile.AdmissionReasonCode = managedkubernetes.ReasonPortForwardCapacity
	}
	return profile, nil
}

// kubernetesPortForwardOpenIntent binds original user slot/OCC before allocating random refs, allowing exact metadata-only replay without another OPEN.
// kubernetesPortForwardOpenIntent связывает original user slot/OCC до random ref allocation, разрешая exact metadata-only replay без нового OPEN.
func kubernetesPortForwardOpenIntent(input command.KubernetesPortForwardConnectionOpen, version int64) string {
	raw, _ := json.Marshal(struct {
		Invocation, Stream string
		Slot               uint32
		Version            int64
	}{input.InvocationRef, input.StreamRef, input.Slot, version})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// readKubernetesPortForwardReadback returns original connection/submission metadata after one storage intent, including a canceled caller, without reissuing bytes.
// readKubernetesPortForwardReadback возвращает original connection/submission metadata после storage intent, включая canceled caller, без повторной выдачи bytes.
func (repository *Repository) readKubernetesPortForwardReadback(ctx context.Context, current scope, p value.Principal, input query.KubernetesPortForwardConnectionRead) (result entity.KubernetesPortForwardConnectionReadback, resultErr error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	parent, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	row, err := readKubernetesPortForwardConnection(ctx, tx, current, parent, input.ConnectionRef, input.OpenIdempotencyKey)
	if err != nil {
		return result, err
	}
	key := row.openKey
	if input.OpenIdempotencyKey == "" && row.closeKey != "" {
		key = row.closeKey
	}
	stage, err := readKubernetesInputStage(ctx, tx, current.organizationID, parent.id, current.actorID, key, parent.view)
	if err != nil || stage.id == "" {
		if err == nil {
			err = errs.ErrUnavailable
		}
		return result, err
	}
	result.Connection, result.Submission = row.view, &stage.metadata
	owned := parent.view
	result.Input = &owned
	if tx.Commit(ctx) != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, errs.ErrUnavailable
	}
	return result, nil
}

// GetKubernetesPortForwardConnection applies current original-root visibility even after grant loss; exact keys recover metadata only.
// GetKubernetesPortForwardConnection применяет current original-root visibility даже после grant loss; exact keys восстанавливают лишь metadata.
func (repository *Repository) GetKubernetesPortForwardConnection(ctx context.Context, p value.Principal, input query.KubernetesPortForwardConnectionRead) (entity.KubernetesPortForwardConnectionReadback, error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.port-forward.get"); err != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.KubernetesPortForwardConnectionReadback{}, err
	}
	return repository.readKubernetesPortForwardReadback(ctx, current, p, input)
}

// ListKubernetesPortForwardConnections scopes every cursor and history row to the same authenticated original actor/stream; missing cursors never restart at the first row.
// ListKubernetesPortForwardConnections связывает cursor и history rows с authenticated original actor/stream; missing cursors не запускают обход сначала.
func (repository *Repository) ListKubernetesPortForwardConnections(ctx context.Context, p value.Principal, input query.KubernetesPortForwardConnectionList) (result entity.KubernetesPortForwardConnectionPage, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.port-forward.list"); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	parent, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	result.Profile, err = readKubernetesPortForwardProfile(ctx, tx, current, parent)
	if err != nil {
		return result, err
	}
	size := input.Page.Size
	if size < 1 {
		size = 16
	}
	if size > 32 {
		return result, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryKubernetesPortForwardList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id, "actor_id": current.actorID, "after_ref": input.Page.Token, "limit": size + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var refs []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if len(refs) > int(size) {
		refs = refs[:size]
		result.Next = refs[len(refs)-1]
	}
	result.Items = make([]entity.KubernetesPortForwardConnection, 0, len(refs))
	for _, ref := range refs {
		row, err := readKubernetesPortForwardConnection(ctx, tx, current, parent, ref, "")
		if err != nil {
			return entity.KubernetesPortForwardConnectionPage{}, err
		}
		result.Items = append(result.Items, row.view)
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesPortForwardConnectionPage{}, errs.ErrUnavailable
	}
	return result, nil
}

// lockKubernetesPortForwardAdmission preserves source-before-Connection ordering and reconstructs the same current original binding used by ordinary INPUT.
// It returns no execution credit; the protected staged tuple must still pass Begin INPUT before native OPEN.
// lockKubernetesPortForwardAdmission сохраняет source-before-Connection порядок и восстанавливает current original binding обычного INPUT.
// Execution credit не выдаётся; protected staged tuple отдельно проходит Begin INPUT до native OPEN.
func (repository *Repository) lockKubernetesPortForwardAdmission(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, inv, stream string) (kubernetesInputRecord, error) {
	var id, connection, run, node string
	originErr := tx.QueryRow(ctx, queryKubernetesInputPublicOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": inv, "project_ref": p.ProjectRef}).Scan(&id, &connection, &run, &node)
	if errors.Is(originErr, pgx.ErrNoRows) {
		return kubernetesInputRecord{}, errs.ErrNotFound
	}
	if originErr != nil {
		return kubernetesInputRecord{}, errs.ErrUnavailable
	}
	owner, _, fence, generation, err := repository.readKubernetesChildGateOwnerTx(ctx, tx, current, inv)
	if err != nil {
		return kubernetesInputRecord{}, err
	}
	record, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, inv, stream)
	if err != nil {
		return record, err
	}
	snapshot, err := kubernetesOperationSnapshotDigestFromClaim(owner, fence, generation)
	if err != nil || snapshot != record.snapshotDigest || generation != record.view.Generation {
		return kubernetesInputRecord{}, errs.ErrForbidden
	}
	if record.view.Operation != "kubernetes.port-forward" {
		return kubernetesInputRecord{}, errs.ErrForbidden
	}
	return record, nil
}

// OpenKubernetesPortForwardConnection assigns one incarnation only after exact original-root/current-source checks and persistent Open-key lookup.
// The committed OPEN uses the existing single-attempt INPUT object pipeline; storage uncertainty never allocates a replacement connection.
// OpenKubernetesPortForwardConnection назначает incarnation только после exact original-root/current-source checks и persistent Open-key lookup.
// Committed OPEN использует existing single-attempt INPUT object pipeline; storage uncertainty не создаёт replacement connection.
func (repository *Repository) OpenKubernetesPortForwardConnection(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesPortForwardConnectionOpen) (result entity.KubernetesPortForwardConnectionReadback, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.port-forward.open"); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || !validInputSubmissionKey(mutation.IdempotencyKey) || input.Slot >= managedkubernetes.MaximumForwardPorts {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	parent, err := repository.lockKubernetesPortForwardAdmission(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	intent := kubernetesPortForwardOpenIntent(input, *mutation.ExpectedVersion)
	prior, lookupErr := readKubernetesPortForwardConnection(ctx, tx, current, parent, "", mutation.IdempotencyKey)
	if lookupErr == nil {
		if prior.openIntent != intent {
			return result, errs.ErrIdempotencyReuse
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return repository.readKubernetesPortForwardReadback(ctx, current, p, query.KubernetesPortForwardConnectionRead{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, OpenIdempotencyKey: mutation.IdempotencyKey})
	}
	if !errors.Is(lookupErr, errs.ErrNotFound) {
		return result, lookupErr
	}
	profile, err := readKubernetesPortForwardProfile(ctx, tx, current, parent)
	if err != nil {
		return result, err
	}
	if profile.AdmissionReasonCode != "" {
		return result, errs.ErrConflict
	}
	selected, err := managedkubernetes.DecodePortForwardProfile(parent.input)
	if err != nil || int(input.Slot) >= len(selected.Ports) {
		return result, errs.ErrInvalid
	}
	if parent.view.Version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	var generation int64
	if err := tx.QueryRow(ctx, queryKubernetesPortForwardGeneration, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id}).Scan(&generation); err != nil {
		return result, errs.ErrConflict
	}
	ref, err := newRef("kpf")
	if err != nil {
		return result, err
	}
	payload, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{Action: managedkubernetes.PortForwardConnectionOpen, Slot: byte(input.Slot), Generation: uint64(generation), ConnectionRef: ref})
	if err != nil {
		return result, errs.ErrInvalid
	}
	defer clear(payload)
	sum := sha256.Sum256(payload)
	submission := command.KubernetesStreamInputSubmit{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Channel: "PORT_FORWARD", Payload: payload, PayloadDigest: hex.EncodeToString(sum[:])}
	reservation, err := reserveKubernetesInputTx(ctx, tx, current, parent, mutation, submission)
	if err != nil {
		return result, err
	}
	// A generic INPUT key cannot be adopted as a connection Open, even when its bytes happened to match.
	// Generic INPUT key нельзя присвоить connection Open, даже при случайном совпадении bytes.
	if reservation.existing != nil {
		return result, errs.ErrIdempotencyReuse
	}
	var connectionID string
	err = tx.QueryRow(ctx, queryKubernetesPortForwardInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "stream_id": parent.id, "actor_id": current.actorID, "generation": generation, "slot": input.Slot, "open_key": mutation.IdempotencyKey, "open_intent_digest": intent, "expected_version": *mutation.ExpectedVersion, "stage_id": reservation.stageID}).Scan(&connectionID)
	if err != nil {
		return result, mapWriteError(err)
	}
	if err := bindKubernetesPortForwardInputTx(ctx, tx, current, parent, reservation, connectionID, "OPEN", submission); err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if _, err := repository.finishKubernetesInputReservation(ctx, current, p, reservation, submission, mutation.IdempotencyKey); err != nil {
		return result, err
	}
	readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer readCancel()
	return repository.readKubernetesPortForwardReadback(readCtx, current, p, query.KubernetesPortForwardConnectionRead{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, OpenIdempotencyKey: mutation.IdempotencyKey})
}

// bindKubernetesPortForwardInputTx seals the existing stage sequence and whole canonical digest without introducing another native queue or credit.
// bindKubernetesPortForwardInputTx закрепляет existing stage sequence и whole canonical digest без новой native queue либо credit.
func bindKubernetesPortForwardInputTx(ctx context.Context, tx pgx.Tx, current scope, parent kubernetesInputRecord, reservation kubernetesInputReservation, connectionID, action string, input command.KubernetesStreamInputSubmit) error {
	_, err := tx.Exec(ctx, queryKubernetesPortForwardInputBind, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id, "sequence": parent.sequence + 1, "connection_id": connectionID, "action": action, "payload_digest": input.PayloadDigest, "byte_count": len(input.Payload), "stage_id": reservation.stageID})
	return mapWriteError(err)
}

// CloseKubernetesPortForwardConnection records exact owner closure under current root visibility even after execution authority has disappeared.
// Only a separately verified current source and idle OPEN connection may queue a native CLOSE; all other closures preserve unknown native status without touching siblings.
// CloseKubernetesPortForwardConnection сохраняет exact owner closure под current root visibility даже после утраты execution authority.
// Только отдельно проверенные current source и idle OPEN connection ставят native CLOSE; остальные closure сохраняют unknown native status без изменения siblings.
func (repository *Repository) CloseKubernetesPortForwardConnection(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesPortForwardConnectionClose) (result entity.KubernetesPortForwardConnectionReadback, resultErr error) {
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.port-forward.close"); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || !validInputSubmissionKey(mutation.IdempotencyKey) || !managedkubernetes.ValidPortForwardConnectionRef(input.ConnectionRef) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	parent, admissionErr := repository.lockKubernetesPortForwardAdmission(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	eligible := admissionErr == nil
	if !eligible {
		// Failed execution selection is never converted into native authority. Reacquire only visibility locks in a new transaction.
		// Неуспешный execution selection не преобразуется в native authority. В новой transaction берутся только visibility locks.
		_ = tx.Rollback(ctx)
		tx, err = repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return result, errs.ErrUnavailable
		}
		parent, err = repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
		if err != nil {
			_ = tx.Rollback(ctx)
			return result, err
		}
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	row, err := readKubernetesPortForwardConnection(ctx, tx, current, parent, input.ConnectionRef, "")
	if err != nil {
		return result, err
	}
	raw, _ := json.Marshal(struct {
		Invocation, Stream, Connection string
		Version                        int64
	}{input.InvocationRef, input.StreamRef, input.ConnectionRef, *mutation.ExpectedVersion})
	sum := sha256.Sum256(raw)
	intent := hex.EncodeToString(sum[:])
	const operation = "platform.integrations.kubernetes.port-forward.close"
	var storedDigest string
	var stored []byte
	receiptErr := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, operation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if receiptErr == nil {
		if storedDigest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		result.Connection = row.view
		owned := parent.view
		result.Input = &owned
		if tx.Commit(ctx) != nil {
			return entity.KubernetesPortForwardConnectionReadback{}, errs.ErrUnavailable
		}
		return result, nil
	}
	if !errors.Is(receiptErr, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if parent.view.Version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	if eligible && parent.view.State == "ACTIVE" && (row.view.State == "PREPARED" || row.view.State == "OPENING" || row.view.State == "OPEN" && parent.view.Pending != nil) {
		return result, errs.ErrConflict
	}
	var reservation kubernetesInputReservation
	var submission command.KubernetesStreamInputSubmit
	if row.view.State == "OPEN" && eligible && parent.view.Pending == nil && parent.view.State == "ACTIVE" && parent.invocationState == "RUNNING" {
		payload, encodeErr := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{Action: managedkubernetes.PortForwardConnectionClose, Slot: byte(row.view.Slot), Generation: uint64(row.view.Generation), ConnectionRef: row.view.Ref})
		if encodeErr != nil {
			return result, errs.ErrUnavailable
		}
		defer clear(payload)
		digest := sha256.Sum256(payload)
		submission = command.KubernetesStreamInputSubmit{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Channel: "PORT_FORWARD", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:])}
		reservation, err = reserveKubernetesInputTx(ctx, tx, current, parent, mutation, submission)
		if err != nil {
			return result, err
		}
		if reservation.existing != nil {
			return result, errs.ErrIdempotencyReuse
		}
		if err := bindKubernetesPortForwardInputTx(ctx, tx, current, parent, reservation, row.id, "CLOSE", submission); err != nil {
			return result, err
		}
	}
	if row.view.State == "PREPARED" || row.view.State == "OPENING" || row.view.State == "OPEN" {
		if _, err := tx.Exec(ctx, queryKubernetesPortForwardClose, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": parent.id, "connection_id": row.id, "stage_id": reservation.stageID, "reason_code": managedkubernetes.ReasonStreamCanceled}); err != nil {
			return result, mapWriteError(err)
		}
	}
	row, err = readKubernetesPortForwardConnection(ctx, tx, current, parent, input.ConnectionRef, "")
	if err != nil {
		return result, err
	}
	stored, err = json.Marshal(struct{ Ref string }{row.view.Ref})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, operation, mutation.IdempotencyKey, intent, "KUBERNETES_PORT_FORWARD_CONNECTION", stored); err != nil {
		return result, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if reservation.stageID != "" {
		if _, err := repository.finishKubernetesInputReservation(ctx, current, p, reservation, submission, mutation.IdempotencyKey); err != nil {
			return result, err
		}
	}
	readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer readCancel()
	result, err = repository.readKubernetesPortForwardReadback(readCtx, current, p, query.KubernetesPortForwardConnectionRead{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, ConnectionRef: input.ConnectionRef})
	if reservation.stageID == "" {
		result.Submission = nil
	}
	return result, err
}
