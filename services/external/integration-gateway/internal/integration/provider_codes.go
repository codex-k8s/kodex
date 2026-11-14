package integration

const (
	// queryProfileUnsupportedCode distinguishes a valid but deliberately unsupported query construct from malformed input or credential failure.
	// queryProfileUnsupportedCode отличает valid, но deliberately unsupported query construct от malformed input либо credential failure.
	queryProfileUnsupportedCode = "INTEGRATION_QUERY_PROFILE_UNSUPPORTED"
	// providerResponseInvalidCode rejects a bounded but unverifiable provider response without exposing upstream diagnostics.
	// providerResponseInvalidCode отклоняет ограниченный, но непроверяемый ответ без выдачи upstream diagnostics.
	providerResponseInvalidCode = "INTEGRATION_RESPONSE_INVALID"
	// providerConfigurationInvalidCode closes an invalid endpoint or server-owned resource binding before provider execution.
	// providerConfigurationInvalidCode закрывает некорректный endpoint либо server-owned resource binding до исполнения.
	providerConfigurationInvalidCode = "INTEGRATION_CONFIGURATION_INVALID"
	// providerRequestRejectedCode represents a known refused input or resource boundary, not an unknown completed effect.
	// providerRequestRejectedCode обозначает известный отказ по input/resource boundary, а не неизвестный завершённый effect.
	providerRequestRejectedCode = "INTEGRATION_REQUEST_REJECTED"
	// providerCapabilityUnsupportedCode denies a tuple or mandatory execution profile absent from the closed provider registry.
	// providerCapabilityUnsupportedCode запрещает tuple либо обязательный execution profile вне закрытого provider registry.
	providerCapabilityUnsupportedCode = "INTEGRATION_CAPABILITY_UNSUPPORTED"
	// providerUnavailableCode preserves transient transport unavailability without returning raw provider errors.
	// providerUnavailableCode сохраняет transient transport unavailability без выдачи raw provider errors.
	providerUnavailableCode = "INTEGRATION_UNAVAILABLE"
	// providerAuthRejectedCode classifies an observed authentication denial while retaining only its bounded HTTP status separately.
	// providerAuthRejectedCode классифицирует наблюдённый auth denial с отдельным ограниченным HTTP status.
	providerAuthRejectedCode = "INTEGRATION_AUTH_REJECTED"
)
