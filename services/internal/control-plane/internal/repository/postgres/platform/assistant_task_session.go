package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
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

// queryAssistantTaskSessionSources lists at most 129 visible runs of the anchor session with
// session/storage versions, root refs and latest message-event sequence, ordered by source ref.
// Exact organization, authority project and run.view predicates constrain sources; callers reject
// overflow and inconsistent snapshots.
// queryAssistantTaskSessionSources выбирает до 129 видимых run сессии исходного run с версиями
// сессии и хранилища, корнями и последним номером события сообщения по ссылке источника. Точные
// организация, проект полномочий и run.view ограничивают источники; вызывающий отклоняет
// переполнение и несогласованные снимки.
//
//go:embed sql/assistant_task_session__sources.sql
var queryAssistantTaskSessionSources string

// queryAssistantTaskSessionMessages reads eleven newest published message/execution pairs after an
// offset from visible runs of the exact anchor session. It returns canonical-lineage evidence and
// authoritative USER text for validation; a row or cursor alone does not prove message validity or
// grant access.
// queryAssistantTaskSessionMessages читает одиннадцать последних опубликованных пар
// message/execution после смещения из видимых run точной сессии. Возвращает свидетельство
// согласованного происхождения и авторитетный USER текст для проверки; строка либо курсор сами не
// доказывают корректность и не выдают доступ.
//
//go:embed sql/assistant_task_session__messages.sql
var queryAssistantTaskSessionMessages string

// assistantTaskSessionMaximumMessages limits each returned page to ten whole messages; an extra
// SQL row detects whether another page is needed. It does not truncate an individual message.
// assistantTaskSessionMaximumMessages ограничивает страницу десятью целыми сообщениями;
// дополнительная SQL-строка обнаруживает продолжение. Отдельное сообщение не усекается.
const assistantTaskSessionMaximumMessages = 10

// assistantTaskSessionEncodedTextBudget caps the sum of JSON-encoded message text at 256 KiB,
// reserving envelope room below 512 KiB. An oversized first message fails closed rather than
// returning truncated text.
// assistantTaskSessionEncodedTextBudget ограничивает сумму JSON-кодированного текста 256 KiB,
// оставляя запас общей оболочки до 512 KiB. Слишком большое первое сообщение вызывает отказ вместо
// усечения текста.
const assistantTaskSessionEncodedTextBudget = 256 << 10

// assistantTaskSessionMaximumOffset bounds decoded cursors and generated continuation offsets at
// 10000. It is a pagination work limit, not a source version or authorization token.
// assistantTaskSessionMaximumOffset ограничивает смещения входных и создаваемых курсоров 10000.
// Это предел работы пагинации, а не версия источника или разрешение доступа.
const assistantTaskSessionMaximumOffset = 10000

// assistantTaskSessionCursor stores cursor format version, actor/organization/project/run/session
// binding, source snapshot digest and offset. Canonical JSON/base64 is checked on read; the
// unsigned digest-bound cursor never assigns actor authority.
// assistantTaskSessionCursor содержит версию формата, привязку субъекта, организации, проекта, run
// и сессии, хеш снимка и смещение. При чтении проверяются канонические JSON и base64;
// неподписанный курсор не назначает полномочия.
type assistantTaskSessionCursor struct {
	Version         int
	Binding, Source string
	Offset          int
}

// assistantTaskSourcePin records one visible source run ref/version, root ref and latest
// published-message sequence for the session snapshot commitment. Ordering comes from SQL; these
// pins identify read content and do not authorize launch or resume.
// assistantTaskSourcePin фиксирует ссылку и версию видимого источника, корень и последний номер
// опубликованного сообщения для хеша снимка сессии. Порядок задаёт SQL; привязки идентифицируют
// читаемое содержимое, не разрешая запуск и возобновление.
type assistantTaskSourcePin struct {
	Ref      string
	Version  int64
	Root     string
	Sequence int64
}

// assistantTaskSessionDecode decodes one JSON value with unknown fields rejected and requires EOF
// on a second decode. Failure becomes Unavailable; duplicate object keys are not explicitly
// rejected here, and callers additionally check canonical cursor bytes or message lineage.
// assistantTaskSessionDecode декодирует одно JSON-значение с отклонением неизвестных полей и
// требует EOF при повторном Decode. Ошибка становится Unavailable; повторные ключи явно не
// отклоняются, канонические байты курсора либо происхождение сообщения проверяет вызывающий.
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

// decodeAssistantTaskSessionCursor treats an empty token as offset zero; otherwise enforces size,
// canonical unpadded URL-safe base64/JSON, version 1, offset bounds and exact actor binding.
// Structural/binding errors are Invalid while a changed source digest is VersionMismatch; no
// cursor field is trusted as authority.
// decodeAssistantTaskSessionCursor считает пустой токен нулевым смещением; иначе проверяет размер,
// канонические base64 без padding и JSON, версию 1, пределы смещения и точную привязку субъекта.
// Ошибки формы и привязки — Invalid, изменённый хеш источника — VersionMismatch; поля курсора не
// являются полномочиями.
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

// encodeAssistantTaskSessionCursor encodes the fixed version-1 cursor as unpadded URL-safe base64
// JSON with the supplied binding/source/offset. Marshal errors remain ignored for this fixed
// primitive shape; callers enforce the offset bound and cursor disclosure boundary.
// encodeAssistantTaskSessionCursor кодирует фиксированный курсор версии 1 в URL-safe base64 JSON
// без padding с переданными привязкой, источником и смещением. Ошибка Marshal для примитивной
// формы по-прежнему игнорируется; предел и раскрытие курсора контролирует вызывающий.
func encodeAssistantTaskSessionCursor(binding, source string, offset int) string {
	raw, _ := json.Marshal(assistantTaskSessionCursor{1, binding, source, offset})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// ReadAssistantTaskSession executes the complete lease-locked session read through the existing
// five-second bounded transaction retry. Each retry starts a fresh snapshot after confirmed
// rollback; it preserves the requested run and cursor and performs no launch/resume admission.
// ReadAssistantTaskSession выполняет полное чтение с блокировкой lease через существующий повтор
// транзакции с общим пределом пять секунд. После подтверждённого rollback попытка создаёт свежий
// снимок, сохраняя run и курсор; разрешение запуска и возобновления не проверяется.
func (repository *Repository) ReadAssistantTaskSession(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, input query.AssistantTaskSessionRead) (query.AssistantTaskSessionPage, error) {
	return retryAssistantLockedRead(ctx, func(attempt context.Context) (query.AssistantTaskSessionPage, error) {
		return repository.readAssistantTaskSessionOnce(attempt, principal, leaseRef, fence, generation, input)
	})
}

// readAssistantTaskSessionOnce opens RepeatableRead without SQL ReadOnly because the lease
// requires FOR SHARE, resolves the persisted actor/project and run.view, and validates bounded
// session/source pins before decoding the cursor. It checks canonical message/execution lineage,
// UTF-8, USER source equality or callback safe text, whole-message count and encoded-text budgets.
// Rows close before nested reads; commit and bounded rollback retain the existing retry/error
// policy. Returns no partial page on failure and performs no DML or runtime admission.
// readAssistantTaskSessionOnce открывает RepeatableRead без SQL ReadOnly ради FOR SHARE lease,
// разрешает сохранённые actor/project и run.view, проверяет ограниченные привязки сессии и
// источников до курсора. Проверяет происхождение message/execution, UTF-8, точный USER источник
// либо безопасный callback текст, число целых сообщений и бюджет кодированного текста. Rows
// закрываются до вложенных чтений; commit и ограниченный rollback сохраняют прежние ошибки и
// повтор. Ошибка не возвращает частичную страницу; DML и runtime admission отсутствуют.
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
	_, err = repository.resolveAssistantReadLease(ctx, tx, &current, leaseRef, fence, generation)
	if err != nil {
		return zero, err
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
	binding := jsonValueDigest([]string{current.organizationID, current.actorID, current.authorityProjectID, run.Ref, run.SessionRef})
	result.SourceSHA256 = jsonValueDigest(struct {
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
	// publishedRow holds a message projection, raw message/execution JSON, nullable canonical SQL
	// evidence and authoritative USER text after rows are buffered and closed. None of these fields is
	// returned before decoding and owner/lineage/text-budget validation.
	// publishedRow содержит проекцию сообщения, JSON message/execution, nullable свидетельство
	// согласованности SQL и авторитетный USER текст после буферизации и закрытия rows. Поля не
	// выдаются до декодирования, проверки владельца, происхождения и бюджета текста.
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
