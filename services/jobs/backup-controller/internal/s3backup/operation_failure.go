package s3backup

import (
	"context"
	"errors"
	"net"
	"net/http"

	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// operationFailure retains only a closed failure class, discarding SDK messages, URLs and response metadata.
// RU: operationFailure сохраняет только закрытый класс отказа, отбрасывая сообщения SDK, URL и метаданные ответа.
type operationFailure struct {
	class string
}

// Error renders the closed diagnostic without exposing or unwrapping the native SDK error.
// RU: Error формирует закрытую диагностику без раскрытия или разворачивания исходной ошибки SDK.
func (failure operationFailure) Error() string {
	return "S3 operation failed: " + failure.class
}

// classifyOperationFailure inspects typed causes before sanitization; its fixed classes do not authorize retries.
// RU: classifyOperationFailure исследует типизированные причины до очистки; фиксированные классы не разрешают повторов.
func classifyOperationFailure(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		switch status := response.HTTPStatusCode(); {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return "access_denied"
		case status == http.StatusTooManyRequests:
			return "throttled"
		case status >= 500 && status <= 599:
			return "server"
		default:
			return "http_other"
		}
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "network_timeout"
		}
		return "network"
	}
	return "unknown"
}
