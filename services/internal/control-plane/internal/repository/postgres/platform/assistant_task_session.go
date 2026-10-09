package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_task_session__sources.sql
var queryAssistantTaskSessionSources string

//go:embed sql/assistant_task_session__messages.sql
var queryAssistantTaskSessionMessages string

const assistantTaskSessionMaximumMessages = 10

// Запас на typed metadata/digests/cursor оставляет общий envelope меньше 512KiB.
const assistantTaskSessionEncodedTextBudget = 256 << 10
const assistantTaskSessionMaximumOffset = 10000

type assistantTaskSessionCursor struct {
	Version         int
	Binding, Source string
	Offset          int
}
type assistantTaskSourcePin struct {
	Ref      string
	Version  int64
	Root     string
	Sequence int64
}

func assistantTaskSessionHash(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func assistantTaskSessionDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errs.ErrUnavailable
	}
	if !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return errs.ErrUnavailable
	}
	return nil
}
func decodeAssistantTaskSessionCursor(token, binding, source string) (int, error) {
	if token == "" {
		return 0, nil
	}
	if len(token) > 1024 {
		return 0, errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != token {
		return 0, errs.ErrInvalid
	}
	var cursor assistantTaskSessionCursor
	if assistantTaskSessionDecode(raw, &cursor) != nil || cursor.Version != 1 || cursor.Offset < 1 || cursor.Offset > assistantTaskSessionMaximumOffset || cursor.Binding != binding {
		return 0, errs.ErrInvalid
	}
	canonical, _ := json.Marshal(cursor)
	if !bytes.Equal(raw, canonical) {
		return 0, errs.ErrInvalid
	}
	if cursor.Source != source {
		return 0, errs.ErrVersionMismatch
	}
	return cursor.Offset, nil
}
func encodeAssistantTaskSessionCursor(binding, source string, offset int) string {
	raw, _ := json.Marshal(assistantTaskSessionCursor{1, binding, source, offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (repository *Repository) ReadAssistantTaskSession(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input query.AssistantTaskSessionRead) (query.AssistantTaskSessionPage, error) {
	return retryAssistantLockedRead(ctx, func(attempt context.Context) (query.AssistantTaskSessionPage, error) {
		return repository.readAssistantTaskSessionOnce(attempt, principal, leaseRef, fence, generation, input)
	})
}

func (repository *Repository) readAssistantTaskSessionOnce(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input query.AssistantTaskSessionRead) (_ query.AssistantTaskSessionPage, resultError error) {
	zero := query.AssistantTaskSessionPage{}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return zero, err
	}
	// FOR SHARE запрещён в SQL READ ONLY; транзакция логически read-only, без DML.
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	fenceDigest := sha256.Sum256([]byte(fence))
	var assistantProjectRef string
	err = tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": leaseRef, "fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation}).Scan(&current.actorRef, &current.actorID, &current.authorityProjectID, &assistantProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, errs.ErrNotFound
	}
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if _, err = repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef); err != nil {
		return zero, err
	}
	// Тот же single-resource run.view predicate; не вызываем launch/resume admission.
	run, err := scanRun(tx.QueryRow(ctx, queryQueriesGetrunSelectRunsOrganizationIdRefProjectId, current.organizationID, input.RunRef, current.actorID, current.authorityProjectID), true)
	if err != nil {
		return zero, assistantLockedReadError(err, err)
	}
	result := query.AssistantTaskSessionPage{RunRef: run.Ref, ProjectRef: run.ProjectRef, SessionRef: run.SessionRef, Title: run.Title, State: run.State, RunVersion: run.Version, ResultSummary: run.ResultSummary, SafeErrorCode: run.SafeErrorCode, SafeErrorMessage: run.SafeErrorMessage, Messages: []query.AssistantTaskPublishedMessage{}}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_ref": run.Ref, "actor_id": current.actorID, "authority_project_id": current.authorityProjectID}
	rows, err := tx.Query(ctx, queryAssistantTaskSessionSources, args)
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	pins := []assistantTaskSourcePin{}
	var sessionVersion int64
	for rows.Next() {
		var pin assistantTaskSourcePin
		var sessionRef, state, storage string
		var version int64
		if rows.Scan(&sessionRef, &version, &state, &storage, &pin.Ref, &pin.Version, &pin.Root, &pin.Sequence) != nil || sessionRef != run.SessionRef || version < 1 || (state != "ACTIVE" && state != "CLOSED") || !validSessionReadinessStorage(storage) || pin.Version < 1 || pin.Sequence < 0 || len(pins) == 128 {
			rows.Close()
			return zero, errs.ErrUnavailable
		}
		if sessionVersion != 0 && (sessionVersion != version || result.SessionStorageState != storage) {
			rows.Close()
			return zero, errs.ErrUnavailable
		}
		sessionVersion = version
		result.SessionStorageState = storage
		pins = append(pins, pin)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if len(pins) == 0 {
		return zero, errs.ErrNotFound
	}
	// Actor и область входят в commitment курсора, но не назначаются его payload.
	binding := assistantTaskSessionHash([]string{current.organizationID, current.actorID, current.authorityProjectID, run.Ref, run.SessionRef})
	result.SourceSHA256 = assistantTaskSessionHash(struct {
		Binding                    string
		SessionVersion, RunVersion int64
		Storage                    string
		Pins                       []assistantTaskSourcePin
	}{binding, sessionVersion, run.Version, result.SessionStorageState, pins})
	offset, err := decodeAssistantTaskSessionCursor(input.Cursor, binding, result.SourceSHA256)
	if err != nil {
		return zero, err
	}
	args["offset"] = offset
	rows, err = tx.Query(ctx, queryAssistantTaskSessionMessages, args)
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	type publishedRow struct {
		item                     query.AssistantTaskPublishedMessage
		messageRaw, executionRaw []byte
		canonical                *bool
		userText                 *string
	}
	candidates := []publishedRow{}
	for rows.Next() {
		var candidate publishedRow
		item := &candidate.item
		if rows.Scan(&item.EventRef, &item.EventSequence, &candidate.messageRaw, &candidate.executionRaw, &item.SourceRunRef, &item.SourceRunVersion, &item.SessionRef, &item.NodeRef, &item.TurnRef, &item.TurnNumber, &candidate.canonical, &candidate.userText) != nil {
			rows.Close()
			return zero, errs.ErrUnavailable
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	texts := 0
	for _, candidate := range candidates {
		item, messageRaw, executionRaw, canonical, userText := candidate.item, candidate.messageRaw, candidate.executionRaw, candidate.canonical, candidate.userText
		if canonical == nil || !*canonical || len(messageRaw) > 512<<10 || len(executionRaw) > 4096 {
			return zero, errs.ErrUnavailable
		}
		var message entity.RunMessage
		var execution entity.RunEventExecution
		if assistantTaskSessionDecode(messageRaw, &message) != nil || assistantTaskSessionDecode(executionRaw, &execution) != nil || execution.RunRef != item.SourceRunRef || execution.SessionRef != run.SessionRef || execution.NodeRef != item.NodeRef || execution.TurnRef != item.TurnRef || execution.TurnNumber != item.TurnNumber || execution.Attempt < 1 || message.Revision != 1 || !runtimeActivityRefPattern.MatchString(message.Ref) || !utf8.ValidString(message.Text) {
			rows.Close()
			return zero, errs.ErrUnavailable
		}
		item.MessageRef, item.Phase, item.Text, item.Origin, item.MessageRevision, item.Attempt = message.Ref, message.Phase, message.Text, "ORDINARY", message.Revision, execution.Attempt
		if message.Phase == "USER" {
			source, sourceErr := readRuntimeMessageSource(ctx, tx, current.organizationID, item.SourceRunRef, item.TurnRef, item.NodeRef)
			if sourceErr != nil {
				rows.Close()
				return zero, sourceErr
			}
			item.Origin = source.Origin
			if source.Origin == "CALLBACK_CONTINUATION" {
				item.Text = callbackContinuationPublicText
			} else if userText == nil || item.Text != *userText {
				rows.Close()
				return zero, errs.ErrUnavailable
			}
		} else if !validPublishedMessage(&message) {
			rows.Close()
			return zero, errs.ErrUnavailable
		}
		// Никакого усечения отдельного USER/FINAL: следующий message целиком идёт в следующую страницу.
		encoded, _ := json.Marshal(item.Text)
		if len(result.Messages) == assistantTaskSessionMaximumMessages || texts+len(encoded) > assistantTaskSessionEncodedTextBudget {
			if len(result.Messages) == 0 {
				rows.Close()
				return zero, errs.ErrUnavailable
			}
			result.Truncated = true
			break
		}
		texts += len(encoded)
		result.Messages = append(result.Messages, item)
	}
	if result.Truncated {
		next := offset + len(result.Messages)
		if next > assistantTaskSessionMaximumOffset {
			return zero, errs.ErrUnavailable
		}
		result.NextCursor = encodeAssistantTaskSessionCursor(binding, result.SourceSHA256, next)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return result, nil
}
