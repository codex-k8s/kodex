// Package artifactread carries bounded owner-download diagnostics without retaining raw adapter errors or resource coordinates.
// Package artifactread переносит ограниченную диагностику чтения владельцем без сохранения сырых ошибок адаптера и координат ресурса.
package artifactread

import (
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

const (
	// MaximumAttempts caps owner artifact-read attempts at eight and bounds diagnostic attempt values; mutation retry has a separate policy.
	// MaximumAttempts ограничивает попытки чтения артефакта владельцем восемью и задаёт предел диагностического счётчика; повтор изменений имеет отдельную политику.
	MaximumAttempts = 8

	// StageUnknown is the fallback for absent or unsupported download stages after normalization, preventing arbitrary stage labels.
	// StageUnknown обозначает отсутствие либо неподдержанный этап чтения после нормализации, исключая произвольные метки этапов.
	StageUnknown = "UNCLASSIFIED"
	// StageScope identifies failure while resolving the owner-download scope, without including actor or resource coordinates.
	// StageScope обозначает отказ разрешения области чтения владельцем без actor и координат ресурса.
	StageScope = "SCOPE"
	// StageBegin identifies failure to begin the download transaction rather than a denied resource eligibility decision.
	// StageBegin обозначает отказ открытия транзакции чтения, а не отрицательное решение о доступности ресурса.
	StageBegin = "BEGIN"
	// StageSelect identifies selection of the exact artifact revision before content retrieval.
	// StageSelect обозначает выбор точной ревизии артефакта до получения содержимого.
	StageSelect = "SELECT_REVISION"
	// StageMetadata identifies artifact metadata loading or validation, without retaining the metadata itself.
	// StageMetadata обозначает загрузку или проверку метаданных артефакта без сохранения самих метаданных.
	StageMetadata = "ARTIFACT_METADATA"
	// StageEligibility identifies the owner-download eligibility check; the diagnostic stage grants no access.
	// StageEligibility обозначает проверку доступности чтения владельцу; диагностическая метка доступа не предоставляет.
	StageEligibility = "ELIGIBILITY"
	// StageRevision identifies revision metadata processing separately from selection of the revision.
	// StageRevision обозначает обработку метаданных ревизии отдельно от её выбора.
	StageRevision = "REVISION_METADATA"
	// StageGrantRef identifies generation of the reference for the content-read grant, never its actual token value.
	// StageGrantRef обозначает создание ссылки разрешения чтения содержимого без значения самого токена.
	StageGrantRef = "GRANT_REF"
	// StageGrantCreate identifies creation of the owner-issued content grant within the read path.
	// StageGrantCreate обозначает создание выдаваемого владельцем разрешения на содержимое внутри пути чтения.
	StageGrantCreate = "GRANT_CREATE"
	// StageGrantConsume identifies consumption of the exact content-read grant, independently of storage retrieval.
	// StageGrantConsume обозначает использование точного разрешения на чтение содержимого отдельно от получения данных хранилища.
	StageGrantConsume = "GRANT_CONSUME"
	// StageContent identifies processing of the content receipt before retrieving storage bytes.
	// StageContent обозначает обработку квитанции содержимого до получения байтов хранилища.
	StageContent = "CONTENT_RECEIPT"
	// StageStorage identifies the storage GET boundary without recording an object key, body or provider error.
	// StageStorage обозначает границу GET хранилища без записи ключа объекта, тела и ошибки провайдера.
	StageStorage = "STORAGE_GET"
	// StageAuditRef identifies generation of a download audit reference rather than audit insertion itself.
	// StageAuditRef обозначает создание ссылки аудита чтения отдельно от вставки аудита.
	StageAuditRef = "AUDIT_REF"
	// StageAudit identifies insertion of the download audit record in the owner transaction.
	// StageAudit обозначает вставку записи аудита чтения в транзакции владельца.
	StageAudit = "AUDIT_INSERT"
	// StageCommit identifies transaction commit failure after preceding download work; it does not assert rollback of external reads.
	// StageCommit обозначает отказ фиксации транзакции после предшествующей работы чтения и не утверждает откат внешних чтений.
	StageCommit = "COMMIT"
	// StageRetry identifies exhaustion of the bounded artifact-read retry path rather than an additional resource action.
	// StageRetry обозначает исчерпание ограниченных повторов чтения артефакта, а не дополнительное действие с ресурсом.
	StageRetry = "RETRY_BUDGET"

	// ClassUnknown is the normalized fallback when no supported failure class is available.
	// ClassUnknown обозначает нормализованный запасной класс при отсутствии поддержанной категории отказа.
	ClassUnknown = "UNCLASSIFIED"
	// ClassPostgres identifies a PostgreSQL failure and is the only class permitted to retain a closed SQLSTATE code.
	// ClassPostgres обозначает отказ PostgreSQL и является единственным классом, сохраняющим SQLSTATE из закрытого набора.
	ClassPostgres = "POSTGRES"
	// ClassDriver identifies a database-driver failure without exposing the driver error or query.
	// ClassDriver обозначает отказ драйвера БД без раскрытия его ошибки и запроса.
	ClassDriver = "DRIVER"
	// ClassCanceled identifies caller/lifecycle cancellation separately from a database or domain denial.
	// ClassCanceled обозначает отмену вызывающим либо жизненным циклом отдельно от отказа БД или домена.
	ClassCanceled = "CANCELED"
	// ClassDeadline identifies an exhausted context deadline without storing timing or request data.
	// ClassDeadline обозначает истечение срока контекста без сохранения параметров времени и запроса.
	ClassDeadline = "DEADLINE"
	// ClassTransactionAbort identifies a transaction-abort category distinct from raw PostgreSQL diagnostics.
	// ClassTransactionAbort обозначает категорию прерывания транзакции отдельно от сырых диагностик PostgreSQL.
	ClassTransactionAbort = "TRANSACTION_ABORT"
	// ClassDomain identifies an already mapped domain outcome without carrying domain payload or owner identifiers.
	// ClassDomain обозначает уже классифицированный доменный результат без доменного payload и идентификаторов владельца.
	ClassDomain = "DOMAIN"
	// ClassStorage identifies a storage-adapter failure while suppressing provider errors and object coordinates.
	// ClassStorage обозначает отказ адаптера хранилища с исключением ошибок провайдера и координат объекта.
	ClassStorage = "STORAGE"
	// ClassReference identifies failure to create a required reference without recording entropy or generated identifiers.
	// ClassReference обозначает отказ создания необходимой ссылки без записи энтропии и сгенерированных идентификаторов.
	ClassReference = "REFERENCE"
)

// Diagnostic contains only closed stage/class/SQLSTATE and bounded attempt metadata; raw errors, SQL, arguments, content and resource coordinates have no field here.
// Diagnostic содержит лишь закрытые stage/class/SQLSTATE и ограниченные сведения о попытке; полей для сырых ошибок, SQL, аргументов, содержимого и координат ресурса нет.
type Diagnostic struct {
	Stage, Class, SQLState string
	Attempt                int
	RetryExhausted         bool
}

// failure wraps a canonical domain sentinel with normalized diagnostic metadata; only WithFailure constructs it after dropping an arbitrary adapter error chain.
// failure оборачивает каноническую доменную sentinel-ошибку нормализованной диагностикой; WithFailure создаёт её после удаления произвольной цепочки ошибок адаптера.
type failure struct {
	cause      error
	diagnostic Diagnostic
}

// NewDiagnostic normalizes the supplied stage/class/SQLSTATE immediately, leaving attempt and exhaustion at their zero defaults until explicitly annotated.
// NewDiagnostic сразу нормализует переданные stage/class/SQLSTATE, сохраняя нулевые попытку и признак исчерпания до явного добавления.
func NewDiagnostic(stage, class, sqlState string) Diagnostic {
	return normalize(Diagnostic{Stage: stage, Class: class, SQLState: sqlState})
}

// Error returns only the retained canonical sentinel message; it does not serialize diagnostic fields or the discarded original error.
// Error возвращает лишь сообщение сохранённой канонической sentinel-ошибки, не сериализуя диагностику и отброшенную исходную ошибку.
func (err *failure) Error() string { return err.cause.Error() }

// Unwrap exposes the retained canonical sentinel to errors.Is while keeping discarded adapter causes unreachable.
// Unwrap открывает сохранённую каноническую sentinel-ошибку для errors.Is, не возвращая отброшенные причины адаптера.
func (err *failure) Unwrap() error { return err.cause }

// WithFailure attaches normalized diagnostics only to outcomes matching Unavailable or Conflict, in that precedence order, retaining just the canonical sentinel. Other domain denials and nil are returned unchanged.
// WithFailure прикрепляет нормализованную диагностику лишь к Unavailable либо Conflict в указанном порядке приоритета, сохраняя только каноническую sentinel-ошибку. Другие доменные отказы и nil возвращаются неизменёнными.
func WithFailure(outcome error, diagnostic Diagnostic) error {
	var cause error
	switch {
	case errors.Is(outcome, errs.ErrUnavailable):
		cause = errs.ErrUnavailable
	case errors.Is(outcome, errs.ErrConflict):
		cause = errs.ErrConflict
	default:
		return outcome
	}
	return &failure{cause: cause, diagnostic: normalize(diagnostic)}
}

// WithAttempt annotates only an existing typed diagnostic with attempt/exhaustion and rewraps through normalization; unrelated errors remain identical and out-of-range attempts become zero.
// WithAttempt добавляет attempt/exhaustion лишь к существующей типизированной диагностике и повторно нормализует её; прочие ошибки остаются прежними, попытки вне предела становятся нулём.
func WithAttempt(err error, attempt int, exhausted bool) error {
	diagnostic, ok := From(err)
	if !ok {
		return err
	}
	diagnostic.Attempt, diagnostic.RetryExhausted = attempt, exhausted
	return WithFailure(err, diagnostic)
}

// From recognizes only this package-private failure type through errors.As and normalizes its copied fields again; matching error text or a caller-defined interface cannot mint a diagnostic.
// From распознаёт лишь закрытый тип failure этого пакета через errors.As и повторно нормализует копию полей; совпавший текст ошибки и интерфейс вызывающего диагностику не создают.
func From(err error) (Diagnostic, bool) {
	var source *failure
	if !errors.As(err, &source) {
		return Diagnostic{}, false
	}
	return normalize(source.diagnostic), true
}

// normalize closes stage/class and SQLSTATE sets, uses NONE outside POSTGRES and OTHER for unsupported PostgreSQL codes, and clears attempts outside 0..MaximumAttempts. RetryExhausted is preserved independently of the attempt counter.
// normalize закрывает наборы stage/class и SQLSTATE, назначает NONE вне POSTGRES и OTHER для неподдержанных кодов PostgreSQL, обнуляя попытки вне 0..MaximumAttempts. RetryExhausted сохраняется независимо от счётчика.
func normalize(value Diagnostic) Diagnostic {
	switch value.Stage {
	case StageScope, StageBegin, StageSelect, StageMetadata, StageEligibility,
		StageRevision, StageGrantRef, StageGrantCreate, StageGrantConsume, StageContent,
		StageStorage, StageAuditRef, StageAudit, StageCommit, StageRetry:
	default:
		value.Stage = StageUnknown
	}
	switch value.Class {
	case ClassPostgres, ClassDriver, ClassCanceled, ClassDeadline,
		ClassTransactionAbort, ClassDomain, ClassStorage, ClassReference:
	default:
		value.Class = ClassUnknown
	}
	if value.Class != ClassPostgres {
		value.SQLState = "NONE"
	} else {
		switch value.SQLState {
		case "40001", "40P01", "08006", "23505", "42501", "42P01", "42883", "57014", "53300":
		default:
			value.SQLState = "OTHER"
		}
	}
	if value.Attempt < 0 || value.Attempt > MaximumAttempts {
		value.Attempt = 0
	}
	return value
}
