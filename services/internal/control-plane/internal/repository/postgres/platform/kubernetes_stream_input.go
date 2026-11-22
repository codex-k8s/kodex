package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInputPublicOrigin keeps public_origin inside the original tenant-scoped stream graph.
// queryKubernetesInputPublicOrigin выполняет public_origin внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__public_origin.sql
var queryKubernetesInputPublicOrigin string

// queryKubernetesInputPublicLock keeps public_lock inside the original tenant-scoped stream graph.
// queryKubernetesInputPublicLock выполняет public_lock внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__public_lock.sql
var queryKubernetesInputPublicLock string

// queryKubernetesInputView keeps view inside the original tenant-scoped stream graph.
// queryKubernetesInputView выполняет view внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__view.sql
var queryKubernetesInputView string

// queryKubernetesInputList keeps list inside the original tenant-scoped stream graph.
// queryKubernetesInputList выполняет list внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__list.sql
var queryKubernetesInputList string

// queryKubernetesInputStage keeps stage inside the original tenant-scoped stream graph.
// queryKubernetesInputStage выполняет stage внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__stage.sql
var queryKubernetesInputStage string

// queryKubernetesInputReserve keeps reserve inside the original tenant-scoped stream graph.
// queryKubernetesInputReserve выполняет reserve внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__reserve.sql
var queryKubernetesInputReserve string

// queryKubernetesInputRecord keeps record inside the original tenant-scoped stream graph.
// queryKubernetesInputRecord выполняет record внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__record.sql
var queryKubernetesInputRecord string

// queryKubernetesInputClaim keeps claim inside the original tenant-scoped stream graph.
// queryKubernetesInputClaim выполняет claim внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__claim.sql
var queryKubernetesInputClaim string

// queryKubernetesInputUnknown keeps unknown inside the original tenant-scoped stream graph.
// queryKubernetesInputUnknown выполняет unknown внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__unknown.sql
var queryKubernetesInputUnknown string

// queryKubernetesInputAdmit keeps admit inside the original tenant-scoped stream graph.
// queryKubernetesInputAdmit выполняет admit внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__admit.sql
var queryKubernetesInputAdmit string

// queryKubernetesInputAck keeps ack inside the original tenant-scoped stream graph.
// queryKubernetesInputAck выполняет ack внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__ack.sql
var queryKubernetesInputAck string

// queryKubernetesInputQuota keeps quota inside the original tenant-scoped stream graph.
// queryKubernetesInputQuota выполняет quota внутри исходного tenant-scoped stream graph.
//
//go:embed sql/kubernetes_input__quota.sql
var queryKubernetesInputQuota string

// queryKubernetesInputCurrent checks exact immutable child/pin eligibility without minting a frame.
// queryKubernetesInputCurrent проверяет exact immutable child/pin eligibility без создания frame.
//
//go:embed sql/kubernetes_input__current.sql
var queryKubernetesInputCurrent string

// queryKubernetesInputStreamLock preserves stream-before-stage ordering for late private storage facts.
// queryKubernetesInputStreamLock сохраняет stream-before-stage порядок для late private storage facts.
//
//go:embed sql/kubernetes_input__stream_lock.sql
var queryKubernetesInputStreamLock string

// kubernetesInputAPICaller accepts only the authenticated public route; project scope is checked against the original run.
// kubernetesInputAPICaller допускает authenticated public route; project scope сверяется с original run.
func kubernetesInputAPICaller(p value.Principal, permission string) error {
	if p.Validate() != nil || p.CallerWorkload != "control-api-gateway" || p.Permission != permission {
		return errs.ErrForbidden
	}
	return nil
}

// kubernetesInputRecord holds locked metadata; private bytes never enter this public projection.
// kubernetesInputRecord хранит locked metadata; private bytes не входят в public projection.
type kubernetesInputRecord struct {
	fileSource                                                       *kubernetesFileSourceRecord
	view                                                             entity.KubernetesStreamInputView
	id, invocationID, bindingDigest, snapshotDigest, invocationState string
	input                                                            managedkubernetes.OperationInput
	sequence, logical, delivered                                     uint64
	ceiling, now                                                     time.Time
}

// kubernetesInputStage retains immutable source facts without loading private content.
// kubernetesInputStage хранит immutable source facts без загрузки private content.
type kubernetesInputStage struct {
	metadata                                            entity.KubernetesStreamInputSubmission
	id, key, version, etag, submissionKey, intentDigest string
	deadline, admitted                                  *time.Time
}

// readKubernetesInputStage locks the exact actor key or latest owner sequence, never a payload.
// readKubernetesInputStage блокирует exact actor key либо latest owner sequence, не payload.
func readKubernetesInputStage(ctx context.Context, tx pgx.Tx, org, stream, actor, key string, view entity.KubernetesStreamInputView) (kubernetesInputStage, error) {
	var stage kubernetesInputStage
	err := tx.QueryRow(ctx, queryKubernetesInputStage, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "actor_id": actor, "submission_key": key}).Scan(&stage.id, &stage.metadata.Ref, &stage.metadata.Sequence, &stage.metadata.Channel, &stage.metadata.PayloadDigest, &stage.metadata.ByteCount, &stage.metadata.EndOfInput, &stage.metadata.State, &stage.key, &stage.version, &stage.etag, &stage.deadline, &stage.admitted, &stage.submissionKey, &stage.intentDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return stage, nil
	}
	if err != nil {
		return stage, errs.ErrUnavailable
	}
	stage.metadata.InvocationRef, stage.metadata.StreamRef, stage.metadata.Generation = view.InvocationRef, view.StreamRef, view.Generation
	if stage.metadata.State == "ATTEMPTING" {
		stage.metadata.State = enum.KubernetesInputUnknown
	}
	if !stage.metadata.State.Known() {
		return kubernetesInputStage{}, errs.ErrUnavailable
	}
	return stage, nil
}

// readKubernetesInputRecord verifies persisted observation and selected target before projecting a view.
// readKubernetesInputRecord сверяет persisted observation и selected target до выдачи view.
func readKubernetesInputRecord(ctx context.Context, tx pgx.Tx, org, inv, stream string) (kubernetesInputRecord, error) {
	var record kubernetesInputRecord
	var observation, raw []byte
	v := &record.view
	v.MaximumStagedBytes = managedkubernetes.MaximumStagedBytes
	v.MaximumStagedObjects = managedkubernetes.MaximumStagedObjects
	err := tx.QueryRow(ctx, queryKubernetesInputView, pgx.StrictNamedArgs{"organization_id": org, "invocation_ref": inv, "stream_ref": stream}).Scan(&record.id, &v.InvocationRef, &v.StreamRef, &v.RunRef, &v.NodeRef, &v.Generation, &v.Operation, &v.State, &v.Version, &v.InputChannels, &v.ClosedInputChannels, &v.MaximumFrameBytes, &v.MaximumTotalBytes, &v.AcknowledgedSequence, &v.HardDeadline, &observation, &raw, &record.sequence, &record.logical, &record.delivered, &record.bindingDigest, &record.snapshotDigest, &record.ceiling, &record.now, &v.TargetRef, &v.TargetRevision, &v.ConnectionRef, &record.invocationState)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrNotFound
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	var witness managedkubernetes.ChildCandidate
	if json.Unmarshal(observation, &witness) != nil || witness.Validate() != nil {
		return record, errs.ErrUnavailable
	}
	var decodeErr error
	record.input, _, decodeErr = managedkubernetes.DecodeOperationInput(v.Operation, raw)
	clear(raw)
	if decodeErr != nil {
		return record, errs.ErrUnavailable
	}
	v.Resource = witness.Resource
	v.Namespace = witness.Resource.Namespace
	if v.Operation == "kubernetes.watch" || v.Operation == "kubernetes.watch.all-namespaces" {
		v.Namespace = "@cluster"
		if witness.Resource.AllNamespaces {
			v.Namespace = "@all-namespaces"
		} else if witness.Resource.Namespaced {
			v.Namespace = witness.Resource.Namespace
		}
	} else {
		v.PodName = witness.Resource.Name
		if witness.PodWitness != nil {
			v.Container = witness.PodWitness.Container
		} else if v.Operation == "kubernetes.logs" {
			var options struct {
				Container string `json:"container"`
			}
			if len(record.input.Options) != 0 && json.Unmarshal(record.input.Options, &options) != nil {
				return record, errs.ErrUnavailable
			}
			v.Container = options.Container
		}
	}
	if managedkubernetes.ValidateStreamInputViewResource(v.Operation, v.Namespace, v.PodName, v.Container, v.Resource) != nil {
		return record, errs.ErrUnavailable
	}
	stage, err := readKubernetesInputStage(ctx, tx, org, record.id, "", "", *v)
	if err != nil {
		return record, err
	}
	if stage.id != "" && stage.metadata.State != enum.KubernetesInputAcknowledged {
		v.Pending = &stage.metadata
	}
	return record, nil
}

// lockPublicKubernetesInput resolves original actor ownership before OCC or idempotency, including closed streams.
// It requires current run and Connection visibility, not an active credential or grant for read/closure.
// lockPublicKubernetesInput разрешает original actor ownership до OCC/idempotency, включая closed streams.
// Требует current run/Connection visibility, не active credential либо grant для read/closure.
func (repository *Repository) lockPublicKubernetesInput(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, inv, stream string) (kubernetesInputRecord, error) {
	id, err := repository.lockPublicKubernetesTool(ctx, tx, current, p, inv)
	if err != nil {
		return kubernetesInputRecord{}, err
	}
	record, err := readKubernetesInputRecord(ctx, tx, current.organizationID, inv, stream)
	record.invocationID = id
	return record, err
}

// GetKubernetesStreamInput returns current safe metadata and an optional original idempotency receipt.
// GetKubernetesStreamInput возвращает current safe metadata и optional original idempotency receipt.
func (repository *Repository) GetKubernetesStreamInput(ctx context.Context, p value.Principal, input query.KubernetesStreamInputRead) (result entity.KubernetesStreamInputReadback, resultErr error) {
	ctx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.input.get"); err != nil {
		return result, err
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	return repository.readKubernetesInputReadback(ctx, current, p, input)
}

// readKubernetesInputReadback shares safe owner response construction without manufacturing a principal.
// readKubernetesInputReadback разделяет safe owner response construction без создания principal.
func (repository *Repository) readKubernetesInputReadback(ctx context.Context, current scope, p value.Principal, input query.KubernetesStreamInputRead) (result entity.KubernetesStreamInputReadback, resultErr error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	record, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	result.View = record.view
	if input.SubmissionIdempotencyKey != "" {
		stage, err := readKubernetesInputStage(ctx, tx, current.organizationID, record.id, current.actorID, input.SubmissionIdempotencyKey, record.view)
		if err != nil {
			return result, err
		}
		if stage.id != "" {
			result.Submission = &stage.metadata
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesStreamInputReadback{}, errs.ErrUnavailable
	}
	return result, nil
}

// ListKubernetesStreamInputs pages exact original-root streams and repeats the exact read predicate per item.
// ListKubernetesStreamInputs выдаёт страницы exact original-root streams с exact read predicate каждого item.
func (repository *Repository) ListKubernetesStreamInputs(ctx context.Context, p value.Principal, input query.KubernetesStreamInputList) (result entity.KubernetesStreamInputPage, resultErr error) {
	ctx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.input.list"); err != nil {
		return result, err
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	limit := input.Page.Size
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 || input.Page.Token != "" && (!strings.HasPrefix(input.Page.Token, "kst_") || !managedkubernetes.ValidLocator(input.Page.Token) || len(input.Page.Token) > 96) {
		return result, errs.ErrInvalid
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: input.RunRef}); err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	rows, err := tx.Query(ctx, queryKubernetesInputList, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "run_ref": input.RunRef, "node_ref": input.NodeRef, "after_ref": input.Page.Token, "limit": limit + 1})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var refs [][2]string
	for rows.Next() {
		var item [2]string
		if rows.Scan(&item[0], &item[1]) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		refs = append(refs, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	if len(refs) > int(limit) {
		refs = refs[:limit]
		result.Next = refs[len(refs)-1][1]
	}
	result.Items = make([]entity.KubernetesStreamInputView, 0, len(refs))
	for _, item := range refs {
		record, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, item[0], item[1])
		if err != nil {
			return entity.KubernetesStreamInputPage{}, err
		}
		result.Items = append(result.Items, record.view)
	}
	if tx.Commit(ctx) != nil {
		return entity.KubernetesStreamInputPage{}, errs.ErrUnavailable
	}
	return result, nil
}

// validateKubernetesInputSource binds public bytes to the registered operation and exact immutable port profile.
// FILE_CONTENT is reserved for the purpose-owned pinned artifact reader and is never public stdin.
// validateKubernetesInputSource связывает public bytes с registered operation и exact immutable port profile.
// FILE_CONTENT оставлен purpose-owned pinned artifact reader и не является public stdin.
func validateKubernetesInputSource(record kubernetesInputRecord, input command.KubernetesStreamInputSubmit) error {
	if !input.ValidPayload() || len(input.Payload) > int(record.view.MaximumFrameBytes) || !slices.Contains(record.view.InputChannels, input.Channel) || slices.Contains(record.view.ClosedInputChannels, input.Channel) {
		return errs.ErrForbidden
	}
	switch input.Channel {
	case "STDIN", "TERMINAL_RESIZE":
		if record.view.Operation != "kubernetes.exec" && record.view.Operation != "kubernetes.attach" && record.view.Operation != "kubernetes.debug" {
			return errs.ErrForbidden
		}
	case "PORT_FORWARD":
		if record.view.Operation != "kubernetes.port-forward" {
			return errs.ErrForbidden
		}
		profile, err := managedkubernetes.DecodePortForwardProfile(record.input)
		frame, frameErr := managedkubernetes.DecodePortForwardConnectionFrame(input.Payload)
		if err != nil || frameErr != nil || input.EndOfInput || frame.ValidateInput(len(profile.Ports)) != nil {
			return errs.ErrInvalid
		}
	default:
		return errs.ErrForbidden
	}
	return nil
}

// inputSubmissionIntent hashes safe metadata and OCC, binding one idempotency key to an exact submission.
// inputSubmissionIntent хэширует safe metadata и OCC, связывая key с exact submission.
func inputSubmissionIntent(input command.KubernetesStreamInputSubmit, version int64) string {
	raw, _ := json.Marshal(struct {
		Invocation, Stream, Channel, Digest string
		EOF                                 bool
		Version                             int64
	}{input.InvocationRef, input.StreamRef, input.Channel, input.PayloadDigest, input.EndOfInput, version})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// SubmitKubernetesStreamInput commits one private intent before one object PUT; unknown writes never retry.
// No event carries private bytes; exact keyed Get is the authoritative submission receipt path.
// SubmitKubernetesStreamInput фиксирует private intent до одного object PUT; unknown writes не повторяются.
// События с private bytes нет; exact keyed Get является authoritative receipt path.
func (repository *Repository) SubmitKubernetesStreamInput(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesStreamInputSubmit) (result entity.KubernetesStreamInputReadback, resultErr error) {
	ctx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.input.submit"); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || !validInputSubmissionKey(mutation.IdempotencyKey) || !input.ValidPayload() {
		return result, errs.ErrInvalid
	}
	input.Payload = bytes.Clone(input.Payload)
	defer clear(input.Payload)
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var ownedID, ownedConnection, ownedRun, ownedNode string
	originErr := tx.QueryRow(ctx, queryKubernetesInputPublicOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": input.InvocationRef, "project_ref": p.ProjectRef}).Scan(&ownedID, &ownedConnection, &ownedRun, &ownedNode)
	if errors.Is(originErr, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if originErr != nil {
		return result, errs.ErrUnavailable
	}
	owner, _, fence, generation, err := repository.readKubernetesChildGateOwnerTx(ctx, tx, current, input.InvocationRef)
	if err != nil {
		return result, err
	}
	record, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	snapshot, err := kubernetesOperationSnapshotDigestFromClaim(owner, fence, generation)
	if err != nil || snapshot != record.snapshotDigest || generation != record.view.Generation {
		return result, errs.ErrForbidden
	}
	if err := validateKubernetesInputSource(record, input); err != nil {
		return result, err
	}
	connectionID, action, err := validateKubernetesPortForwardPublicInputTx(ctx, tx, current, record, input)
	if err != nil {
		return result, err
	}
	reservation, err := reserveKubernetesInputTx(ctx, tx, current, record, mutation, input)
	if err != nil {
		return result, err
	}
	if connectionID != "" && reservation.existing == nil {
		if err := bindKubernetesPortForwardInputTx(ctx, tx, current, record, reservation, connectionID, action, input); err != nil {
			return result, err
		}
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if reservation.existing != nil {
		return *reservation.existing, nil
	}
	return repository.finishKubernetesInputReservation(ctx, current, p, reservation, input, mutation.IdempotencyKey)
}

// validInputSubmissionKey rejects ambiguous control bytes before any receipt lookup.
// validInputSubmissionKey отклоняет ambiguous control bytes до receipt lookup.
func validInputSubmissionKey(key string) bool {
	return len(key) >= 8 && len(key) <= 128 && utf8.ValidString(key) && !strings.ContainsAny(key, "\r\n\x00")
}

// recordKubernetesInputObject retains late exact storage facts under the canonical stream-before-stage lock order.
// recordKubernetesInputObject сохраняет late exact storage facts под canonical stream-before-stage lock order.
func (repository *Repository) recordKubernetesInputObject(ctx context.Context, org, stream, id, state, version, etag string) (resultErr error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := tx.Exec(ctx, queryKubernetesInputStreamLock, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream}); err != nil {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryKubernetesInputRecord, pgx.StrictNamedArgs{"organization_id": org, "stage_id": id, "state": state, "object_version": version, "object_etag": etag})
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	if tx.Commit(ctx) != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// StopKubernetesStream closes only the original tool invocation and dependencies without credential admission.
// Known accepted facts remain unchanged; the response does not assert a native process join.
// StopKubernetesStream закрывает лишь original tool invocation/dependencies без credential admission.
// Known accepted facts не меняются; response не утверждает native process join.
func (repository *Repository) StopKubernetesStream(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesStreamInputStop) (result entity.KubernetesStreamInputView, resultErr error) {
	ctx, cancelRequest := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRequest()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.input.stop"); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || !validInputSubmissionKey(mutation.IdempotencyKey) {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	record, err := repository.lockPublicKubernetesInput(ctx, tx, current, p, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	intent := inputSubmissionIntent(command.KubernetesStreamInputSubmit{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Channel: "STOP"}, *mutation.ExpectedVersion)
	var storedDigest string
	var stored []byte
	receiptErr := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, "platform.integrations.kubernetes.input.stop", mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	if receiptErr == nil {
		if storedDigest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return record.view, nil
	}
	if !errors.Is(receiptErr, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if record.view.Version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	if record.invocationState == "RUNNING" {
		if _, err := tx.Exec(ctx, queryKubernetesInvocationCancel, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": record.invocationID}); err != nil {
			return result, mapWriteError(err)
		}
	}
	record, err = readKubernetesInputRecord(ctx, tx, current.organizationID, input.InvocationRef, input.StreamRef)
	if err != nil {
		return result, err
	}
	stored, err = json.Marshal(record.view)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, "platform.integrations.kubernetes.input.stop", mutation.IdempotencyKey, intent, "KUBERNETES_STREAM", stored); err != nil {
		return result, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return record.view, nil
}
