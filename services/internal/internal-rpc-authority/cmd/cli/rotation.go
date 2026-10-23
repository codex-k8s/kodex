package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// rotationStatusSQL embeds the operator rotation-status query that returns JSON for local shape validation; embedding alone does not observe or mutate a rotation.
// rotationStatusSQL встраивает операторский запрос статуса ротации, возвращающий JSON для локальной проверки формы; само встраивание не наблюдает и не изменяет ротацию.
//
//go:embed sql/rotation__status.sql
var rotationStatusSQL string

// rotationAbortSQL embeds the specialized rotation-abort query, called with exact intent, source revision and digest parameters rather than interpolated SQL.
// rotationAbortSQL встраивает специализированный запрос отмены ротации, вызываемый с точными параметрами намерения, исходной ревизии и дайджеста без подстановки SQL.
//
//go:embed sql/rotation__abort.sql
var rotationAbortSQL string

// rotationDigestPattern accepts exactly sixty-four lowercase hexadecimal characters for rotation digest syntax. It does not rehash content, authenticate provenance or accept an uppercase digest.
// rotationDigestPattern принимает ровно шестьдесят четыре шестнадцатеричных символа нижнего регистра для синтаксиса дайджеста ротации. Содержимое не хешируется повторно, происхождение не подтверждается, верхний регистр не принимается.
var rotationDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// rotationOptions holds the parsed watch operation ID or abort intent/revision/digest, with only the action-relevant fields populated. These selectors are correlation inputs, not caller authority or a substitute for the database's lifecycle checks.
// rotationOptions хранит разобранный идентификатор наблюдаемой операции либо намерение, ревизию и дайджест отмены с заполнением только нужных действию полей. Это входы сопоставления, а не полномочия вызывающего и не замена проверкам жизненного цикла в БД.
type rotationOptions struct {
	intentID     string
	operationID  string
	revision     int64
	sourceDigest string
}

// rotationStatus models JSON observations for an empty rotation, a single intent or a compound operation, preserving optional revisions, deadlines, phase, completion and readback counts through pointers and omitempty tags. readRotationStatus applies conditional validation; the DTO itself is not a signed state transition or durable completion receipt.
// rotationStatus моделирует JSON-наблюдения пустой ротации, отдельного намерения или составной операции, сохраняя необязательные ревизии, сроки, фазу, завершение и счётчики readback через указатели и теги omitempty. Условные проверки выполняет readRotationStatus; сама структура не является подписанным переходом состояния или устойчивой квитанцией завершения.
type rotationStatus struct {
	ObservedAt                 time.Time  `json:"observedAt"`
	OperationID                string     `json:"operationId,omitempty"`
	OperationStatus            string     `json:"operationStatus,omitempty"`
	Phase                      string     `json:"phase,omitempty"`
	IntentID                   string     `json:"intentId,omitempty"`
	ProtocolVersion            int        `json:"protocolVersion,omitempty"`
	SourceRevision             int64      `json:"sourceRevision,omitempty"`
	SourceDigestSHA256         string     `json:"sourceDigestSHA256,omitempty"`
	RegistrySourceDigestSHA256 string     `json:"registrySourceDigestSHA256,omitempty"`
	RegistryRevision           *int64     `json:"registryRevision,omitempty"`
	BaseRevision               *int64     `json:"baseRevision,omitempty"`
	BaseDigestSHA256           string     `json:"baseDigestSHA256,omitempty"`
	SwitchNotBefore            *time.Time `json:"switchNotBefore,omitempty"`
	PreviousNotAfter           *time.Time `json:"previousNotAfter,omitempty"`
	CompletedAt                *time.Time `json:"completedAt,omitempty"`
	PredecessorRevision        *int64     `json:"predecessorRevision,omitempty"`
	PredecessorDigestSHA256    string     `json:"predecessorDigestSHA256,omitempty"`
	DeliveryStartedAt          *time.Time `json:"deliveryStartedAt,omitempty"`
	DeliveredAt                *time.Time `json:"deliveredAt,omitempty"`
	PromotedAt                 *time.Time `json:"promotedAt,omitempty"`
	OverlapUntil               *time.Time `json:"overlapUntil,omitempty"`
	RetiredAt                  *time.Time `json:"retiredAt,omitempty"`
	AbortedAt                  *time.Time `json:"abortedAt,omitempty"`
	Status                     string     `json:"status"`
	ExpectedReadbacks          *int       `json:"expectedReadbackCount,omitempty"`
	ActualReadbacks            *int       `json:"actualReadbackCount,omitempty"`
}

// parseRotationOptions validates exact watch arguments and a canonical nonzero operation UUID, or exact abort flags/confirmation, canonical nonzero intent UUID, canonical positive JavaScript-safe decimal revision and lowercase digest. Other actions return empty options; accepted syntax does not authorize a database abort.
// parseRotationOptions проверяет точные аргументы наблюдения и канонический ненулевой UUID операции либо точные флаги и подтверждение отмены, канонический ненулевой UUID намерения, каноническую положительную десятичную ревизию в безопасном для JavaScript диапазоне и дайджест нижнего регистра. Другие действия получают пустые параметры; принятый синтаксис не разрешает отмену в БД.
func parseRotationOptions(action command, args []string) (rotationOptions, error) {
	if action == commandRotationWatch {
		if len(args) != 3 || args[1] != "--operation-id" {
			return rotationOptions{}, errors.New("invalid authority rotation watch arguments")
		}
		id, err := uuid.Parse(args[2])
		if err != nil || id == uuid.Nil || id.String() != args[2] {
			return rotationOptions{}, errors.New("invalid authority rotation operation identity")
		}
		return rotationOptions{operationID: args[2]}, nil
	}
	if action != commandRotationAbort {
		return rotationOptions{}, nil
	}
	if len(args) != 9 || args[1] != "--intent-id" || args[3] != "--source-revision" ||
		args[5] != "--source-digest-sha256" || args[7] != "--confirm" ||
		args[8] != "ABORT-STAGING-AUTHORITY-ROTATION" {
		return rotationOptions{}, errors.New("invalid authority rotation abort arguments")
	}
	id, idErr := uuid.Parse(args[2])
	revision, revisionErr := strconv.ParseInt(args[4], 10, 64)
	if idErr != nil || id == uuid.Nil || id.String() != args[2] ||
		revisionErr != nil || revision < 1 || revision > 9007199254740991 ||
		strconv.FormatInt(revision, 10) != args[4] || !rotationDigestPattern.MatchString(args[6]) {
		return rotationOptions{}, errors.New("invalid authority rotation abort identity")
	}
	return rotationOptions{intentID: args[2], revision: revision, sourceDigest: args[6]}, nil
}

// readRotationStatus decodes one database JSON result and requires an observation time, closed state sets and the action-dependent intent or operation bindings. Compound operations require canonical identity, consistent state, bounded adjacent registry/base revisions, expected-readback count, digest syntax and phase/deadline/completion presence; nonempty single intents require supported protocol, source revision and digest. It uses json.Unmarshal rather than a strict unknown-field decoder and does not verify signatures, all optional timestamps or actual readback completeness.
// readRotationStatus декодирует один JSON-результат БД и требует время наблюдения, закрытые наборы состояний и зависящие от формы привязки намерения либо операции. Для составной операции нужны каноническая идентичность, согласованное состояние, ограниченные соседние ревизии реестра и базы, ожидаемое число readback, синтаксис дайджестов и присутствие фазы, сроков и завершения; непустому отдельному намерению нужны поддерживаемый протокол, исходная ревизия и дайджест. Используется json.Unmarshal, а не строгий декодер неизвестных полей; подписи, все необязательные времена и фактическая полнота readback не проверяются.
func readRotationStatus(ctx context.Context, db *sql.DB) (rotationStatus, error) {
	var raw []byte
	if err := db.QueryRowContext(ctx, rotationStatusSQL).Scan(&raw); err != nil {
		return rotationStatus{}, errors.New("authority rotation readback failed")
	}
	var result rotationStatus
	decodeErr := json.Unmarshal(raw, &result)
	allowed := map[string]bool{
		"EMPTY": true, "PREPARED": true, "DELIVERING": true,
		"DELIVERED": true, "PROMOTED": true, "RETIRED": true, "ABORTED": true,
		"DISTRIBUTING": true, "WAITING_SWITCH": true, "SWITCHING": true,
		"WAITING_RETIRE": true, "RETIRING": true,
	}
	operationAllowed := map[string]bool{
		"DISTRIBUTING": true, "WAITING_SWITCH": true, "SWITCHING": true,
		"WAITING_RETIRE": true, "RETIRING": true, "RETIRED": true,
	}
	operationID, operationIDErr := uuid.Parse(result.OperationID)
	if decodeErr != nil || result.ObservedAt.IsZero() || !allowed[result.Status] ||
		(result.Status == "EMPTY" && result.IntentID != "") ||
		(result.OperationID != "" && (operationIDErr != nil || operationID == uuid.Nil ||
			operationID.String() != result.OperationID || !operationAllowed[result.OperationStatus] ||
			result.OperationStatus != result.Status ||
			result.RegistryRevision == nil || result.BaseRevision == nil ||
			*result.RegistryRevision < 1 || *result.RegistryRevision > 9007199254740991 ||
			*result.BaseRevision < 1 || *result.BaseRevision > 9007199254740988 ||
			*result.RegistryRevision != *result.BaseRevision+1 ||
			result.ExpectedReadbacks == nil || *result.ExpectedReadbacks < 1 || *result.ExpectedReadbacks > 384 ||
			!rotationDigestPattern.MatchString(result.RegistrySourceDigestSHA256) ||
			!rotationDigestPattern.MatchString(result.BaseDigestSHA256) ||
			(result.Phase != "" && result.Phase != "DISTRIBUTE" && result.Phase != "SWITCH" && result.Phase != "RETIRE") ||
			((result.OperationStatus == "WAITING_SWITCH" || result.OperationStatus == "SWITCHING" ||
				result.OperationStatus == "WAITING_RETIRE" || result.OperationStatus == "RETIRING" ||
				result.OperationStatus == "RETIRED") && result.SwitchNotBefore == nil) ||
			((result.OperationStatus == "SWITCHING" || result.OperationStatus == "WAITING_RETIRE" ||
				result.OperationStatus == "RETIRING" || result.OperationStatus == "RETIRED") && result.PreviousNotAfter == nil) ||
			(result.OperationStatus == "RETIRED" && result.CompletedAt == nil))) ||
		(result.Status != "EMPTY" && result.OperationID == "" && (result.IntentID == "" ||
			(result.ProtocolVersion != 1 && result.ProtocolVersion != 2) ||
			result.SourceRevision < 1 || !rotationDigestPattern.MatchString(result.SourceDigestSHA256))) {
		return rotationStatus{}, errors.New("authority rotation readback rejected")
	}
	return result, nil
}

// runRotation gives status/abort a sixty-second context and watch two hundred eighty seconds. Abort executes once, reports an uncertain outcome on query failure without retry and verifies exact intent plus ABORTED readback; watch emits observations at five-second intervals until the requested operation is RETIRED. Writer or timeout errors stop the command and do not roll back committed abort state or authorize a new intent.
// runRotation задаёт статусу и отмене контекст шестьдесят секунд, наблюдению — двести восемьдесят. Отмена выполняется один раз, ошибка запроса возвращает неопределённость исхода без повтора, затем проверяются точное намерение и ABORTED; наблюдение выводит записи с интервалом пять секунд до RETIRED запрошенной операции. Ошибки вывода или срока прекращают команду и не откатывают зафиксированную отмену и не разрешают новое намерение.
func runRotation(ctx context.Context, db *sql.DB, action command, options rotationOptions, output io.Writer) error {
	timeout := 60 * time.Second
	if action == commandRotationWatch {
		timeout = 280 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if action == commandRotationAbort {
		var accepted bool
		if err := db.QueryRowContext(ctx, rotationAbortSQL, options.intentID, options.revision, options.sourceDigest).Scan(&accepted); err != nil {
			return errors.New("authority rotation abort outcome uncertain; read rotation-status before any retry")
		}
		if !accepted {
			return errors.New("authority rotation abort rejected")
		}
	}
	if action == commandRotationWatch {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			state, err := readRotationStatus(ctx, db)
			if err != nil {
				return err
			}
			if err := json.NewEncoder(output).Encode(state); err != nil {
				return errors.New("write authority rotation observation")
			}
			if rotationWatchComplete(state, options.operationID) {
				return nil
			}
			select {
			case <-ctx.Done():
				return errors.New("authority rotation observation timeout")
			case <-ticker.C:
			}
		}
	}
	state, err := readRotationStatus(ctx, db)
	if err != nil {
		return err
	}
	if action == commandRotationAbort && (state.IntentID != options.intentID || state.Status != "ABORTED") {
		return errors.New("authority rotation abort readback mismatch")
	}
	if err := json.NewEncoder(output).Encode(state); err != nil {
		return errors.New("write authority rotation readback")
	}
	return nil
}

// rotationWatchComplete returns true only when both the observed operation ID matches the requested ID and its state is RETIRED. Foreign terminal operations and nonterminal observations never satisfy this local completion predicate.
// rotationWatchComplete возвращает true только при совпадении наблюдаемой и запрошенной операции и состоянии RETIRED. Чужая завершённая операция и незавершённое наблюдение не удовлетворяют этому локальному предикату.
func rotationWatchComplete(state rotationStatus, operationID string) bool {
	return state.OperationID == operationID && state.Status == "RETIRED"
}
