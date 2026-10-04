package build

import "errors"

const (
	inputReasonOwnerScope      = "OWNER_SCOPE"
	inputReasonSHASchema       = "INPUT_SHA_SCHEMA"
	inputReasonFrontendPin     = "FRONTEND_PIN"
	inputReasonBaseAllowlist   = "BASE_ALLOWLIST"
	inputReasonBuilderPin      = "BUILDER_PIN"
	inputReasonToolchainPin    = "TOOLCHAIN_PIN"
	inputReasonRuntimeContract = "RUNTIME_CONTRACT_PIN"
	inputReasonContextRef      = "CONTEXT_REF"
	inputReasonOwnerDockerfile = "OWNER_DOCKERFILE"
	inputReasonWorkspaceCreate = "WORKSPACE_CREATE"
	inputRejectedSummary       = "Immutable build input was rejected"
)

// inputRejectionError содержит только закрытую причину, но не вход или filesystem error.
type inputRejectionError struct {
	reason string
	cause  error
}

func (failure inputRejectionError) Error() string { return InputRejectionSummary(failure) }
func (failure inputRejectionError) Unwrap() error { return failure.cause }

func rejectInput(reason string, cause error) error {
	return inputRejectionError{reason: reason, cause: cause}
}

// InputRejectionReason не сериализует исходную ошибку и повторно проверяет whitelist.
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

// InputRejectionSummary сохраняет прежний публичный текст для неизвестной причины.
func InputRejectionSummary(err error) string {
	if reason := InputRejectionReason(err); reason != "" {
		return inputRejectedSummary + " (" + reason + ")"
	}
	return inputRejectedSummary
}
