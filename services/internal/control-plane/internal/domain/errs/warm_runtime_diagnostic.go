package errs

import "errors"

const (
	// WarmReconcileScope labels resolution of the warm-runtime owner scope without embedding the owner identity in the diagnostic.
	// WarmReconcileScope помечает разрешение области владельца тёплого runtime без включения идентичности владельца в диагностику.
	WarmReconcileScope = "SCOPE"
	// WarmReconcileTransaction labels the reconciliation transaction boundary separately from its final commit stage.
	// WarmReconcileTransaction помечает границу транзакции сверки отдельно от её конечного этапа фиксации.
	WarmReconcileTransaction = "TRANSACTION"
	// WarmReconcileProviderPolicy is the general provider-policy stage; more specific already-recorded read, decode, catalog or publish stages remain distinguishable.
	// WarmReconcileProviderPolicy — общий этап политики провайдера; уже записанные более точные этапы чтения, разбора, каталога или публикации остаются различимыми.
	WarmReconcileProviderPolicy = "PROVIDER_POLICY"
	// WarmReconcileProviderPolicyRead labels reading provider policy without exposing the stored policy or adapter error.
	// WarmReconcileProviderPolicyRead помечает чтение политики провайдера без раскрытия сохранённой политики или ошибки адаптера.
	WarmReconcileProviderPolicyRead = "PROVIDER_POLICY_READ"
	// WarmReconcileProviderPolicyDecode labels decoding provider policy separately from fetching or publishing it.
	// WarmReconcileProviderPolicyDecode помечает разбор политики провайдера отдельно от её получения или публикации.
	WarmReconcileProviderPolicyDecode = "PROVIDER_POLICY_DECODE"
	// WarmReconcileProviderPolicyCatalog labels the provider-policy catalog stage without treating catalog contents as diagnostic labels.
	// WarmReconcileProviderPolicyCatalog помечает этап каталога политики провайдера, не превращая содержимое каталога в диагностические метки.
	WarmReconcileProviderPolicyCatalog = "PROVIDER_POLICY_CATALOG"
	// WarmReconcileProviderPolicyPublish identifies the provider-policy publication stage; the label does not assert successful publication.
	// WarmReconcileProviderPolicyPublish обозначает этап публикации политики провайдера; сама метка не утверждает успешную публикацию.
	WarmReconcileProviderPolicyPublish = "PROVIDER_POLICY_PUBLISH"
	// WarmReconcileSessionBinding labels the session-binding stage independently of session replacement; it is not proof of an established session.
	// WarmReconcileSessionBinding помечает этап привязки сессии отдельно от замены сессии; доказательством установленной сессии метка не является.
	WarmReconcileSessionBinding = "SESSION_BINDING"
	// WarmReconcileSessionReplacement labels replacement of a session, preserving a distinct diagnostic from initial binding.
	// WarmReconcileSessionReplacement помечает замену сессии с диагностикой, отличающейся от первоначальной привязки.
	WarmReconcileSessionReplacement = "SESSION_REPLACEMENT"
	// WarmReconcileSnapshot labels assembly of the runtime snapshot without carrying snapshot contents in the error label.
	// WarmReconcileSnapshot помечает сборку снимка runtime без включения содержимого снимка в метку ошибки.
	WarmReconcileSnapshot = "RUNTIME_SNAPSHOT"
	// WarmReconcileOverlay labels the warm-runtime overlay stage separately from environment and snapshot stages.
	// WarmReconcileOverlay помечает этап наложения конфигурации тёплого runtime отдельно от этапов окружения и снимка.
	WarmReconcileOverlay = "OVERLAY"
	// WarmReconcileCatalog identifies the session-catalog stage; it remains distinct from the provider-policy catalog label.
	// WarmReconcileCatalog обозначает этап каталога сессии и отличается от метки каталога политики провайдера.
	WarmReconcileCatalog = "SESSION_CATALOG"
	// WarmReconcileEnvironment labels environment preparation without reflecting environment variables or secret values.
	// WarmReconcileEnvironment помечает подготовку окружения без отражения переменных окружения или значений секретов.
	WarmReconcileEnvironment = "ENVIRONMENT"
	// WarmReconcileLifecycle labels lifecycle reconciliation; it records a diagnostic stage rather than executing a state transition.
	// WarmReconcileLifecycle помечает сверку жизненного цикла; метка фиксирует диагностический этап, а не выполняет переход состояния.
	WarmReconcileLifecycle = "LIFECYCLE"
	// WarmReconcilePrompt labels prompt preparation while keeping prompt text out of the public diagnostic.
	// WarmReconcilePrompt помечает подготовку промпта, не включая его текст в публичную диагностику.
	WarmReconcilePrompt = "PROMPT"
	// WarmReconcileWorkspace labels workspace preparation without exposing filesystem paths or workspace contents in the label.
	// WarmReconcileWorkspace помечает подготовку рабочего пространства без раскрытия файловых путей или его содержимого в метке.
	WarmReconcileWorkspace = "WORKSPACE"
	// WarmReconcileContext labels runtime-context preparation separately from snapshot and prompt assembly.
	// WarmReconcileContext помечает подготовку контекста runtime отдельно от сборки снимка и промпта.
	WarmReconcileContext = "CONTEXT"
	// WarmReconcileSpecification labels runtime specification preparation without embedding the specification in the failure message.
	// WarmReconcileSpecification помечает подготовку спецификации runtime без включения спецификации в сообщение ошибки.
	WarmReconcileSpecification = "SPECIFICATION"
	// WarmReconcileDigest labels the digest stage and does not itself validate or publish a runtime revision.
	// WarmReconcileDigest помечает этап дайджеста и сам по себе не проверяет и не публикует ревизию runtime.
	WarmReconcileDigest = "DIGEST"
	// WarmReconcileCommit labels the final transaction-commit stage independently of earlier preparation failures.
	// WarmReconcileCommit помечает конечный этап фиксации транзакции отдельно от предыдущих ошибок подготовки.
	WarmReconcileCommit = "COMMIT"
)

// warmRuntimeFailure retains a normalized reconciliation stage and original cause while exposing a fixed public failure message.
// warmRuntimeFailure хранит нормализованный этап сверки и исходную причину, показывая фиксированное публичное сообщение ошибки.
type warmRuntimeFailure struct {
	stage string
	cause error
}

// Error returns a fixed warm-runtime reconciliation failure message without reflecting adapter text or stage data.
// Error возвращает фиксированное сообщение ошибки сверки тёплого runtime без отражения текста адаптера или данных этапа.
func (failure *warmRuntimeFailure) Error() string { return "warm runtime reconciliation failed" }

// Unwrap returns the original reconciliation cause so typed error matching survives the sanitized wrapper.
// Unwrap возвращает исходную причину сверки, сохраняя сопоставление типизированных ошибок через очищенную обёртку.
func (failure *warmRuntimeFailure) Unwrap() error { return failure.cause }

// WithWarmRuntimeStage leaves nil untouched and preserves any existing recognized stage found in the error chain. Otherwise it wraps the cause with the normalized supplied stage, keeping errors.Is and errors.As classifications intact.
// WithWarmRuntimeStage оставляет nil неизменным и сохраняет любой уже распознанный этап в цепочке ошибки. Иначе функция оборачивает причину нормализованным переданным этапом, сохраняя классификацию errors.Is и errors.As.
func WithWarmRuntimeStage(err error, stage string) error {
	if err == nil {
		return nil
	}
	if WarmRuntimeStage(err) != "UNKNOWN" {
		return err
	}
	return &warmRuntimeFailure{stage: closedWarmRuntimeStage(stage), cause: err}
}

// WarmRuntimeStage finds a nonnil typed reconciliation wrapper through errors.As and normalizes its stage; absent, typed-nil or unknown stages return UNKNOWN.
// WarmRuntimeStage находит ненулевую типизированную обёртку сверки через errors.As и нормализует этап; отсутствие, типизированный nil или неизвестный этап дают UNKNOWN.
func WarmRuntimeStage(err error) string {
	var failure *warmRuntimeFailure
	if errors.As(err, &failure) && failure != nil {
		return closedWarmRuntimeStage(failure.stage)
	}
	return "UNKNOWN"
}

// closedWarmRuntimeStage accepts only the twenty declared reconciliation labels and maps all other strings to UNKNOWN, bounding diagnostic cardinality and preventing arbitrary text reflection.
// closedWarmRuntimeStage принимает только двадцать объявленных меток сверки и заменяет остальные строки на UNKNOWN, ограничивая число диагностических значений и исключая отражение произвольного текста.
func closedWarmRuntimeStage(stage string) string {
	switch stage {
	case WarmReconcileScope, WarmReconcileTransaction, WarmReconcileProviderPolicy,
		WarmReconcileProviderPolicyRead, WarmReconcileProviderPolicyDecode,
		WarmReconcileProviderPolicyCatalog, WarmReconcileProviderPolicyPublish,
		WarmReconcileSessionBinding, WarmReconcileSessionReplacement,
		WarmReconcileSnapshot, WarmReconcileOverlay, WarmReconcileCatalog,
		WarmReconcileEnvironment, WarmReconcileLifecycle, WarmReconcilePrompt,
		WarmReconcileWorkspace, WarmReconcileContext, WarmReconcileSpecification,
		WarmReconcileDigest, WarmReconcileCommit:
		return stage
	default:
		return "UNKNOWN"
	}
}
