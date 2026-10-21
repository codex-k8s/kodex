package build

import "errors"

const (
	// inputFetchRejectedDiagnostic is the existing public stage for invalid input
	// metadata or workspace setup, separate from the closed detailed reason below.
	// inputFetchRejectedDiagnostic — прежний публичный этап отказа input metadata
	// либо workspace setup, отдельно от закрытой подробной причины ниже.
	inputFetchRejectedDiagnostic = "INPUT_FETCH_REJECTED"
	// inputDigestMismatchDiagnostic preserves the public fetch/integrity failure
	// stage without exposing HTTP, filesystem or private upstream error details.
	// inputDigestMismatchDiagnostic сохраняет публичный этап ошибки fetch/integrity,
	// не раскрывая HTTP, filesystem и закрытые подробности удалённой ошибки.
	inputDigestMismatchDiagnostic = "INPUT_DIGEST_MISMATCH"
	// inputReasonOwnerScope classifies the first owner/scope shape rejection without
	// publishing the input's organization or project locator.
	// inputReasonOwnerScope обозначает первый отказ формы owner/scope без раскрытия
	// organization либо project locator входа.
	inputReasonOwnerScope = "OWNER_SCOPE"
	// inputReasonSHASchema classifies malformed immutable digest syntax, not a
	// successful verification of the referenced file or image bytes.
	// inputReasonSHASchema обозначает неверный синтаксис immutable digest, не успешную
	// проверку байтов файла либо образа по ссылке.
	inputReasonSHASchema = "INPUT_SHA_SCHEMA"
	// inputReasonFrontendPin identifies disagreement with the configured frontend pin.
	// inputReasonFrontendPin обозначает несовпадение с настроенным frontend pin.
	inputReasonFrontendPin = "FRONTEND_PIN"
	// inputReasonBaseAllowlist identifies an unapproved exact base repository/digest pair.
	// inputReasonBaseAllowlist обозначает недопущенную точную пару base repository/digest.
	inputReasonBaseAllowlist = "BASE_ALLOWLIST"
	// inputReasonBuilderPin identifies disagreement with the configured builder pin.
	// inputReasonBuilderPin обозначает несовпадение с настроенным builder pin.
	inputReasonBuilderPin = "BUILDER_PIN"
	// inputReasonToolchainPin identifies disagreement with the configured toolchain pin.
	// inputReasonToolchainPin обозначает несовпадение с настроенным toolchain pin.
	inputReasonToolchainPin = "TOOLCHAIN_PIN"
	// inputReasonRuntimeContract identifies a runtime contract revision/digest mismatch.
	// inputReasonRuntimeContract обозначает несовпадение revision/digest runtime contract.
	inputReasonRuntimeContract = "RUNTIME_CONTRACT_PIN"
	// inputReasonContextRef classifies invalid immutable input source/repository shape.
	// inputReasonContextRef обозначает неверную форму immutable input source/repository.
	inputReasonContextRef = "CONTEXT_REF"
	// inputReasonOwnerDockerfile covers rejected owner Dockerfile/install-block shape,
	// without echoing the submitted script or claiming general code safety.
	// inputReasonOwnerDockerfile обозначает отказ формы owner Dockerfile/install block,
	// не раскрывая переданный скрипт и не объявляя код в целом безопасным.
	inputReasonOwnerDockerfile = "OWNER_DOCKERFILE"
	// inputReasonWorkspaceCreate classifies root-shape or workspace-creation failures
	// without carrying private filesystem paths into the public summary.
	// inputReasonWorkspaceCreate обозначает отказ root shape либо workspace creation
	// без переноса закрытых файловых путей в публичное сообщение.
	inputReasonWorkspaceCreate = "WORKSPACE_CREATE"
	// inputRejectedSummary preserves the fixed fallback for missing/unrecognized
	// reasons, independently of private underlying error text.
	// inputRejectedSummary сохраняет постоянный текст для отсутствующей/неизвестной
	// причины независимо от закрытого текста исходной ошибки.
	inputRejectedSummary = "Immutable build input was rejected"
)

// inputRejectionError retains a reason candidate and private cause. Error projects
// only a rechecked whitelist reason; Unwrap keeps the cause for internal inspection,
// not for serializing the private chain at a public boundary.
// inputRejectionError хранит candidate причины и закрытую cause. Error выводит лишь
// повторно проверенную whitelist-причину; Unwrap сохраняет cause для внутренней
// проверки, не для сериализации закрытой цепочки публичной границей.
type inputRejectionError struct {
	reason string
	cause  error
}

// Error returns the fixed public summary plus only a recognized reason, never cause.
// Error возвращает постоянное публичное сообщение с известной причиной, не cause.
func (failure inputRejectionError) Error() string { return InputRejectionSummary(failure) }

// Unwrap exposes the retained cause for errors.Is/As without making its text public.
// Unwrap предоставляет сохранённую cause для errors.Is/As, не публикуя её текст.
func (failure inputRejectionError) Unwrap() error { return failure.cause }

// rejectInput wraps the candidate/cause without normalizing or trusting the reason;
// outward projection always applies the separate closed whitelist.
// rejectInput сохраняет candidate/cause без нормализации и доверия причине;
// публичная проекция всегда применяет отдельный закрытый whitelist.
func rejectInput(reason string, cause error) error {
	return inputRejectionError{reason: reason, cause: cause}
}

// InputRejectionReason finds the first typed rejection through errors.As and returns
// only one of ten closed reasons; nil, unrelated errors or unknown candidates give
// empty without serializing the wrapped chain or private input values.
// InputRejectionReason находит первый typed rejection через errors.As и возвращает
// лишь одну из десяти закрытых причин; nil, посторонняя ошибка и неизвестный candidate
// дают пустую строку без сериализации цепочки и закрытых input values.
func InputRejectionReason(err error) string {
	var failure inputRejectionError
	if !errors.As(err, &failure) {
		return ""
	}
	switch failure.reason {
	case inputReasonOwnerScope, inputReasonSHASchema, inputReasonFrontendPin,
		inputReasonBaseAllowlist, inputReasonBuilderPin, inputReasonToolchainPin,
		inputReasonRuntimeContract, inputReasonContextRef, inputReasonOwnerDockerfile,
		inputReasonWorkspaceCreate:
		return failure.reason
	default:
		return ""
	}
}

// InputRejectionSummary adds a recognized reason to the unchanged public fallback;
// unknown errors never contribute their message, wrapper or cause text.
// InputRejectionSummary добавляет известную причину к прежнему публичному тексту;
// неизвестная ошибка не переносит свой message, wrapper либо cause.
func InputRejectionSummary(err error) string {
	if reason := InputRejectionReason(err); reason != "" {
		return inputRejectedSummary + " (" + reason + ")"
	}
	return inputRejectedSummary
}
