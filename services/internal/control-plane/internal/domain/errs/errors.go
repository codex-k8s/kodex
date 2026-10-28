// Package errs defines stable control-plane error classes and closed diagnostic wrappers that preserve causes without reflecting private payloads.
// Package errs задаёт стабильные классы ошибок control-plane и закрытые диагностические обёртки, сохраняющие причины без отражения закрытых данных.
package errs

import "errors"

var (
	// ErrProviderCatalogTooLarge marks a provider catalog that exceeds the supported bound; callers can classify the failure without reflecting catalog contents.
	// ErrProviderCatalogTooLarge обозначает каталог провайдера сверх поддерживаемого предела; вызывающий различает класс ошибки без отражения содержимого каталога.
	ErrProviderCatalogTooLarge = errors.New("provider catalog exceeds its bound")
	// ErrInvalid classifies rejected input independently of the underlying parser or field diagnostic. It is a sentinel for errors.Is, not a transport status value.
	// ErrInvalid классифицирует отклонённый ввод независимо от диагностики разборщика или поля. Это опорная ошибка для errors.Is, а не значение транспортного статуса.
	ErrInvalid = errors.New("invalid input")
	// ErrUnauthorized marks missing or invalid authentication; it remains distinct from denial of an authenticated principal's permission.
	// ErrUnauthorized обозначает отсутствие или недействительность аутентификации и отличается от отказа в полномочии уже аутентифицированного субъекта.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrFreshAuthenticationRequired identifies an operation requiring fresh authentication rather than accepting the age of the existing session.
	// ErrFreshAuthenticationRequired обозначает операцию, требующую свежей аутентификации вместо принятия возраста существующей сессии.
	ErrFreshAuthenticationRequired = errors.New("fresh authentication required")
	// ErrForbidden classifies denied authority for an operation; wrapping must preserve its identity without exposing private policy diagnostics.
	// ErrForbidden классифицирует отказ в полномочии на операцию; обёртка должна сохранять идентичность ошибки, не раскрывая закрытую диагностику политики.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound classifies an unavailable resource in the current read boundary; the sentinel itself does not distinguish absence from intentionally hidden resources.
	// ErrNotFound классифицирует недоступный ресурс в текущей границе чтения; сама ошибка не различает отсутствие и намеренно скрытый ресурс.
	ErrNotFound = errors.New("not found")
	// ErrConflict marks an operation incompatible with current state, separately from an explicit optimistic-version mismatch.
	// ErrConflict обозначает операцию, несовместимую с текущим состоянием, отдельно от явного несовпадения оптимистической версии.
	ErrConflict = errors.New("conflict")
	// ErrCapabilityRequired marks an operation whose required platform capability is not enabled; it does not authorize enabling that capability.
	// ErrCapabilityRequired обозначает операцию с невключённой обязательной возможностью платформы; права включить эту возможность ошибка не даёт.
	ErrCapabilityRequired = errors.New("required capability is not enabled")
	// ErrAlreadyResolved identifies a resource that has already received its resolution, allowing callers to preserve that boundary instead of resolving it again.
	// ErrAlreadyResolved обозначает ресурс с уже принятым решением и позволяет сохранить эту границу вместо повторного решения.
	ErrAlreadyResolved = errors.New("resource is already resolved")
	// ErrVersionMismatch identifies failed version preconditions for optimistic concurrency; it does not by itself permit a retry against a newer version.
	// ErrVersionMismatch обозначает нарушение предусловий версии при оптимистической конкуренции и само по себе не разрешает повтор с новой версией.
	ErrVersionMismatch = errors.New("version mismatch")
	// ErrIdempotencyReuse marks reuse of an idempotency key with a different intent rather than successful replay of the original operation.
	// ErrIdempotencyReuse обозначает повторное использование ключа идемпотентности с иным намерением, а не успешное воспроизведение исходной операции.
	ErrIdempotencyReuse = errors.New("idempotency key reused with different intent")
	// ErrProtected marks a protected system resource whose requested modification is not permitted by that boundary.
	// ErrProtected обозначает защищённый системный ресурс, изменение которого не допускается соответствующей границей.
	ErrProtected = errors.New("protected system resource")
	// ErrResourceInUse identifies a resource still referenced or used by another operation; the sentinel is not permission to remove those dependencies.
	// ErrResourceInUse обозначает ресурс, на который ещё ссылается или который использует другая операция; ошибка не разрешает удалять эти зависимости.
	ErrResourceInUse = errors.New("resource is in use")
	// ErrUnavailable classifies temporary dependency or operation unavailability while allowing diagnostic wrappers to hide the private underlying cause.
	// ErrUnavailable классифицирует временную недоступность зависимости или операции, позволяя диагностическим обёрткам скрывать закрытую исходную причину.
	ErrUnavailable = errors.New("temporarily unavailable")
	// ErrMailboxPublicationPending distinguishes a mailbox publication still pending from a completed publication or a generic successful operation.
	// ErrMailboxPublicationPending отличает ещё ожидающую публикацию почтового ящика от завершённой публикации либо обычного успешного действия.
	ErrMailboxPublicationPending = errors.New("mailbox publication is pending")
)

const (
	// AssistantCurrentPromptContext is the closed diagnostic stage for assembling the assistant's current prompt context; it carries no prompt text.
	// AssistantCurrentPromptContext — закрытый диагностический этап сборки текущего контекста промпта помощника; текста промпта он не содержит.
	AssistantCurrentPromptContext = "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT"
	// AssistantCurrentConfigurationView identifies the current configuration projection stage without embedding configuration values in the diagnostic.
	// AssistantCurrentConfigurationView обозначает этап проекции текущей конфигурации без включения её значений в диагностику.
	AssistantCurrentConfigurationView = "ASSISTANT_CURRENT_CONFIGURATION_CONFIG_VIEW"
	// AssistantCurrentOwnerCoreRead identifies the owner-core read stage when current assistant configuration is unavailable; it is a label, not an access grant.
	// AssistantCurrentOwnerCoreRead обозначает этап чтения ядра владельца при недоступности текущей конфигурации помощника; это метка, а не полномочие.
	AssistantCurrentOwnerCoreRead = "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_READ"
	// AssistantCurrentOwnerCoreVersion labels the owner-core version stage separately from the read stage so diagnostics remain bounded and specific.
	// AssistantCurrentOwnerCoreVersion помечает этап версии ядра владельца отдельно от этапа чтения для ограниченной и предметной диагностики.
	AssistantCurrentOwnerCoreVersion = "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_VERSION"
	// AssistantCurrentTemplateProjection labels projection of the current assistant template without reflecting template contents or adapter errors.
	// AssistantCurrentTemplateProjection помечает проекцию текущего шаблона помощника без отражения содержимого шаблона или ошибок адаптера.
	AssistantCurrentTemplateProjection = "ASSISTANT_CURRENT_CONFIGURATION_TEMPLATE_PROJECTION"
	// AssistantCurrentUnclassified is the safe fallback for absent or unknown current-configuration stages; arbitrary strings never become diagnostic labels.
	// AssistantCurrentUnclassified — безопасное запасное значение для отсутствующего или неизвестного этапа текущей конфигурации; произвольные строки не становятся диагностическими метками.
	AssistantCurrentUnclassified = "ASSISTANT_CURRENT_CONFIGURATION_UNCLASSIFIED"
)

// assistantCurrentConfigurationFailure stores a closed configuration stage and the original error for unwrapping while exposing only a fixed public error message.
// assistantCurrentConfigurationFailure хранит закрытый этап конфигурации и исходную ошибку для разворачивания, показывая наружу только фиксированное сообщение.
type assistantCurrentConfigurationFailure struct {
	stage string
	cause error
}

// Error returns a fixed current-configuration availability message and deliberately omits the wrapped adapter error and stage.
// Error возвращает фиксированное сообщение о недоступности текущей конфигурации, намеренно исключая исходную ошибку адаптера и этап.
func (failure *assistantCurrentConfigurationFailure) Error() string {
	return "assistant current configuration is temporarily unavailable"
}

// Unwrap returns the original cause so errors.Is and errors.As retain its classification despite the sanitized Error message.
// Unwrap возвращает исходную причину, чтобы errors.Is и errors.As сохраняли её классификацию несмотря на очищенное сообщение Error.
func (failure *assistantCurrentConfigurationFailure) Unwrap() error { return failure.cause }

// WithAssistantCurrentConfigurationStage wraps only errors matching ErrUnavailable, retaining their cause and normalizing the stage through the closed set. Nil and other error classes are returned unchanged; private cause text is not reflected by the wrapper.
// WithAssistantCurrentConfigurationStage оборачивает только ошибки класса ErrUnavailable, сохраняя причину и нормализуя этап через закрытый набор. Nil и другие классы возвращаются неизменными; закрытый текст причины обёртка не отражает.
func WithAssistantCurrentConfigurationStage(err error, stage string) error {
	if !errors.Is(err, ErrUnavailable) {
		return err
	}
	return &assistantCurrentConfigurationFailure{stage: assistantCurrentConfigurationStage(stage), cause: err}
}

// AssistantCurrentConfigurationStage extracts the typed configuration wrapper through errors.As and returns a normalized stage, falling back to AssistantCurrentUnclassified when absent.
// AssistantCurrentConfigurationStage извлекает типизированную обёртку конфигурации через errors.As и возвращает нормализованный этап либо AssistantCurrentUnclassified при отсутствии обёртки.
func AssistantCurrentConfigurationStage(err error) string {
	var failure *assistantCurrentConfigurationFailure
	if errors.As(err, &failure) {
		return assistantCurrentConfigurationStage(failure.stage)
	}
	return AssistantCurrentUnclassified
}

// assistantCurrentConfigurationStage accepts only the five explicit configuration-stage labels and maps every other value to AssistantCurrentUnclassified; it never returns arbitrary caller text.
// assistantCurrentConfigurationStage принимает только пять явных меток этапов конфигурации и заменяет остальные значения на AssistantCurrentUnclassified; произвольный текст вызывающего не возвращается.
func assistantCurrentConfigurationStage(stage string) string {
	switch stage {
	case AssistantCurrentPromptContext, AssistantCurrentConfigurationView, AssistantCurrentOwnerCoreRead,
		AssistantCurrentOwnerCoreVersion, AssistantCurrentTemplateProjection:
		return stage
	default:
		return AssistantCurrentUnclassified
	}
}

const (
	// AssistantPlanHydrate labels hydration of a plan operation; accepted diagnostics associate this stage with an operation index from one through thirty-two.
	// AssistantPlanHydrate помечает наполнение операции плана; допустимая диагностика связывает этот этап с номером операции от одного до тридцати двух.
	AssistantPlanHydrate = "ASSISTANT_PLAN_HYDRATE"
	// AssistantPlanNormalize labels plan normalization as a bounded diagnostic stage, without storing the normalized operation payload.
	// AssistantPlanNormalize помечает нормализацию плана как ограниченный диагностический этап без сохранения нормализованного содержимого операции.
	AssistantPlanNormalize = "ASSISTANT_PLAN_NORMALIZE"
	// AssistantPlanBind labels binding of a plan operation; the label is diagnostic metadata and does not establish a resource or authority binding.
	// AssistantPlanBind помечает привязку операции плана; метка является диагностическими метаданными и не устанавливает привязку ресурса или полномочия.
	AssistantPlanBind = "ASSISTANT_PLAN_BIND"
	// AssistantPlanAuthorize identifies authorization as the rejecting plan stage without carrying policy details or granting authority itself.
	// AssistantPlanAuthorize обозначает авторизацию как этап отклонения плана, не содержа подробностей политики и не предоставляя полномочия.
	AssistantPlanAuthorize = "ASSISTANT_PLAN_AUTHORIZE"
	// AssistantPlanEmpty labels an empty-plan failure and is the only accepted plan stage whose diagnostic operation index must be zero.
	// AssistantPlanEmpty обозначает ошибку пустого плана и является единственным допустимым этапом с обязательным нулевым номером операции в диагностике.
	AssistantPlanEmpty = "ASSISTANT_PLAN_EMPTY"
	// AssistantPlanCommand labels execution of a plan command separately from hydration, normalization, binding and authorization diagnostics.
	// AssistantPlanCommand помечает исполнение команды плана отдельно от диагностики наполнения, нормализации, привязки и авторизации.
	AssistantPlanCommand = "ASSISTANT_PLAN_COMMAND"
)

// assistantPlanFailure stores the closed preparation stage, operation index and cause; its public message omits the operation payload and original diagnostic text.
// assistantPlanFailure хранит закрытый этап подготовки, номер операции и причину; публичное сообщение исключает содержимое операции и исходный диагностический текст.
type assistantPlanFailure struct {
	stage string
	index int
	cause error
}

// Error returns a fixed plan-preparation failure message rather than formatting the cause or operation index.
// Error возвращает фиксированное сообщение об ошибке подготовки плана, не форматируя причину или номер операции.
func (failure *assistantPlanFailure) Error() string { return "assistant plan preparation failed" }

// Unwrap exposes the original plan failure for error classification while leaving the public Error message sanitized.
// Unwrap открывает исходную ошибку плана для классификации, сохраняя очищенное публичное сообщение Error.
func (failure *assistantPlanFailure) Unwrap() error { return failure.cause }

// WithAssistantPlanStage annotates only conflict, version-mismatch or invalid-input errors with a valid closed stage/index pair. It returns other errors and invalid pairs unchanged and does not retain operation parameters.
// WithAssistantPlanStage добавляет допустимую закрытую пару этапа и номера только к конфликту, несовпадению версии или некорректному вводу. Другие ошибки и недопустимые пары возвращаются неизменными; параметры операции не сохраняются.
func WithAssistantPlanStage(err error, stage string, index int) error {
	if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrVersionMismatch) && !errors.Is(err, ErrInvalid) || !validAssistantPlanStage(stage, index) {
		return err
	}
	return &assistantPlanFailure{stage: stage, index: index, cause: err}
}

// AssistantPlanDiagnostic extracts a valid typed plan wrapper and returns its stage, index and bounded VERSION, CONFLICT or INVALID category, in that precedence order. Missing wrappers, invalid pairs or unsupported causes return ok false and empty metadata.
// AssistantPlanDiagnostic извлекает допустимую типизированную обёртку плана и возвращает этап, номер и ограниченный класс VERSION, CONFLICT либо INVALID в таком порядке приоритета. Отсутствующая обёртка, недопустимая пара или неподдерживаемая причина дают ok false и пустые метаданные.
func AssistantPlanDiagnostic(err error) (stage, category string, index int, ok bool) {
	var failure *assistantPlanFailure
	if !errors.As(err, &failure) || !validAssistantPlanStage(failure.stage, failure.index) {
		return "", "", 0, false
	}
	if errors.Is(failure.cause, ErrVersionMismatch) {
		category = "VERSION"
	} else if errors.Is(failure.cause, ErrConflict) {
		category = "CONFLICT"
	} else if errors.Is(failure.cause, ErrInvalid) {
		category = "INVALID"
	} else {
		return "", "", 0, false
	}
	return failure.stage, category, failure.index, true
}

// validAssistantPlanStage permits indices one through thirty-two for the five operation stages and zero only for AssistantPlanEmpty; unknown stages fail closed.
// validAssistantPlanStage допускает номера от одного до тридцати двух для пяти этапов операции и ноль только для AssistantPlanEmpty; неизвестные этапы отклоняются.
func validAssistantPlanStage(stage string, index int) bool {
	switch stage {
	case AssistantPlanHydrate, AssistantPlanNormalize, AssistantPlanBind, AssistantPlanAuthorize, AssistantPlanCommand:
		return index >= 1 && index <= 32
	case AssistantPlanEmpty:
		return index == 0
	default:
		return false
	}
}

// assistantPlanFieldFailure retains one closed rejecting-field label and the original invalid-input cause, without storing the rejected field value.
// assistantPlanFieldFailure хранит одну закрытую метку отклонённого поля и исходную причину некорректного ввода, не сохраняя отклонённое значение поля.
type assistantPlanFieldFailure struct {
	field string
	cause error
}

// Error returns a fixed invalid-plan-input message without reflecting the field name, value or wrapped cause.
// Error возвращает фиксированное сообщение о некорректном вводе плана без отражения имени поля, значения или обёрнутой причины.
func (failure *assistantPlanFieldFailure) Error() string { return "assistant plan input is invalid" }

// Unwrap preserves access to the original field-validation cause for errors.Is and errors.As rather than changing its error boundary.
// Unwrap сохраняет доступ errors.Is и errors.As к исходной причине проверки поля, не меняя границу ошибки.
func (failure *assistantPlanFieldFailure) Unwrap() error { return failure.cause }

// WithAssistantPlanField annotates only ErrInvalid with an exact supported server diagnostic field; unsupported fields and other error classes pass through unchanged. A request key is not automatically a valid diagnostic field.
// WithAssistantPlanField дополняет только ErrInvalid точным поддерживаемым серверным диагностическим полем; неподдерживаемые поля и другие классы проходят неизменными. Ключ запроса не становится допустимым диагностическим полем автоматически.
func WithAssistantPlanField(err error, field string) error {
	if !errors.Is(err, ErrInvalid) || !validAssistantPlanField(field) {
		return err
	}
	return &assistantPlanFieldFailure{field: field, cause: err}
}

// AssistantPlanField returns the closed field from a typed wrapper only when the complete error still matches ErrInvalid and the stored field is valid; otherwise it returns an empty string.
// AssistantPlanField возвращает закрытое поле из типизированной обёртки только если полная ошибка ещё соответствует ErrInvalid и сохранённое поле допустимо; иначе возвращает пустую строку.
func AssistantPlanField(err error) string {
	var failure *assistantPlanFieldFailure
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &failure) || !validAssistantPlanField(failure.field) {
		return ""
	}
	return failure.field
}

// validAssistantPlanField recognizes only the explicit workflow, input-field and step diagnostic labels. Case variants, arbitrary paths and injected extra text are rejected without normalization.
// validAssistantPlanField распознаёт только явные диагностические метки процесса, поля ввода и шага. Варианты регистра, произвольные пути и добавленный текст отклоняются без нормализации.
func validAssistantPlanField(field string) bool {
	switch field {
	case "WORKFLOW_SHAPE", "WORKFLOW_REF", "WORKFLOW_VERSION", "WORKFLOW_BINDING",
		"MAX_CONCURRENCY", "TIMEOUT_SECONDS", "WORKFLOW_TEXT", "INPUT_FIELDS", "STEPS",
		"INPUT_FIELD_KEY", "STEP_SHAPE", "STEP_KEY", "STEPS_GRAPH", "WORKFLOW_DRAFT",
		"STEPS_INSTRUCTIONS", "STEPS_EXPECTED_RESULT", "WORKFLOW_INVARIANTS", "WORKFLOW_TEXT_BUDGET":
		return true
	default:
		return false
	}
}
